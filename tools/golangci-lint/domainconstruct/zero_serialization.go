package domainconstruct

import (
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"
)

type valueUse uint8

const (
	wholeValue valueUse = iota
	jsonFields
	jsonAddressableFields
	outputStorage
)

// Decoding accepts output storage. An input/output parameter still consumes
// its prior contents. Standard JSON encoders consume the selected fields of
// plain envelopes, while other declared encoders may inspect the whole input.
func (c *zeroChecker) argumentUse(call *ssa.CallCommon, index int) valueUse {
	function := callObject(call)
	use := wholeValue
	for _, boundary := range c.contracts.boundariesFor(function, c.policies) {
		for _, input := range boundary.Encode {
			if callArgumentIndex(call, input) == index {
				if boundary.Codec == "json" && input >= 0 && function.Pkg() != nil && function.Pkg().Path() == "encoding/json" {
					return jsonFields
				}
				return wholeValue
			}
		}
		for _, destination := range boundary.Decode {
			if callArgumentIndex(call, destination) == index {
				use = outputStorage
			}
		}
	}
	return use
}

func (c *zeroChecker) useForType(t types.Type, use valueUse) valueUse {
	if !use.json() {
		return use
	}
	if c.policies.lookup(t) != nil {
		return wholeValue
	}
	// A custom hook may inspect ignored fields. Pointer hooks matter when
	// the encoder can address the value.
	for _, name := range []string{"MarshalJSON", "MarshalText"} {
		method, _, _ := types.LookupFieldOrMethod(t, use == jsonAddressableFields, nil, name)
		if function, ok := method.(*types.Func); ok {
			signature := function.Type().(*types.Signature)
			if validEncoder(signature, "json") {
				return wholeValue
			}
		}
	}
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Slice:
		return jsonAddressableFields
	case *types.Map:
		return jsonFields
	}
	return use
}

func (use valueUse) json() bool {
	return use == jsonFields || use == jsonAddressableFields
}

// Only a declared root decoder establishes valid domain state on nil error.
// A plain envelope's nested hooks can be skipped for missing fields, so the
// envelope cannot provide the same guarantee. Failure can leave partial writes.
func (c *zeroChecker) decoderInitializes(instruction ssa.CallInstruction, location address, before flowPoint, context flowContext) bool {
	call := instruction.Common()
	for _, boundary := range c.contracts.boundariesFor(callObject(call), c.policies) {
		if len(boundary.Decode) == 0 || !c.decodingSucceeded(instruction, boundary.ErrorResult, context) {
			continue
		}
		initialized, conflicting := false, false
		for _, index := range boundary.Decode {
			argument := contractArgument(call, index)
			for _, destination := range c.decodeDestinations(argument, before, make(map[ssa.Value]bool)) {
				target, known := c.collectionAddress(destination)
				if !known || !(mutationTarget{address: location}).overlaps(target) {
					continue
				}
				pointer, ok := destination.Type().Underlying().(*types.Pointer)
				if !ok || target.root != location.root || !pathPrefix(location.path, target.path) ||
					!c.policies.lookup(pointer.Elem()).supports(boundary.Codec) ||
					!supportsNativeDecode(pointer.Elem(), boundary.Codec) {
					conflicting = true
				} else {
					initialized = true
				}
			}
		}
		if initialized && !conflicting {
			return true
		}
	}
	return false
}

func (c *zeroChecker) decodingSucceeded(instruction ssa.CallInstruction, errorIndex int, context flowContext) bool {
	call, ok := instruction.(*ssa.Call)
	if !ok || errorIndex < 0 {
		return false
	}
	if call.Call.Signature().Results().Len() == 1 {
		return c.valueTruth(call, context, make(map[ssa.Value]bool)) == isZero
	}
	if references := call.Referrers(); references != nil {
		for _, reference := range *references {
			if extract, ok := reference.(*ssa.Extract); ok && extract.Index == errorIndex && c.valueTruth(extract, context, make(map[ssa.Value]bool)) == isZero {
				return true
			}
		}
	}
	return false
}

func (c *zeroChecker) decoderMayInvalidate(instruction ssa.CallInstruction, storage mutationTarget, before flowPoint, context flowContext) bool {
	call := instruction.Common()
	for _, boundary := range c.contracts.boundariesFor(callObject(call), c.policies) {
		if len(boundary.Decode) == 0 || c.decodingSucceeded(instruction, boundary.ErrorResult, context) {
			continue
		}
		for _, index := range boundary.Decode {
			if value := contractArgument(call, index); value != nil && c.mayReferenceStorage(value, storage, before, make(map[ssa.Value]bool)) {
				return true
			}
		}
	}
	return false
}

// Recover concrete destinations from local boxing and variadic slices. A
// reaching value must be unambiguous across predecessors; stale or branch-only
// pointers never become a successful initialization fact.
func (c *zeroChecker) decodeDestinations(value ssa.Value, point flowPoint, seen map[ssa.Value]bool) []ssa.Value {
	if value == nil || seen[value] {
		return nil
	}
	seen[value] = true
	defer delete(seen, value)
	switch value := value.(type) {
	case *ssa.MakeInterface:
		return c.decodeDestinations(value.X, point, seen)
	case *ssa.ChangeInterface:
		return c.decodeDestinations(value.X, point, seen)
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if location, known := c.collectionAddress(value.X); known {
				if origin := c.storedReference(location, instructionPoint(value)); origin != nil {
					return c.decodeDestinations(origin, point, seen)
				}
			}
		}
	}
	if _, pointer := value.Type().Underlying().(*types.Pointer); pointer {
		return []ssa.Value{value}
	}
	view, ok := c.collectionOf(value)
	if !ok || view.end < 0 || view.dynamicStart {
		return nil
	}
	var destinations []ssa.Value
	visited := make(map[string]bool)
	for _, store := range c.stores[view.storage.root] {
		if !pathPrefix(store.target.path, view.storage.path) || len(store.target.path) != len(view.storage.path)+1 {
			continue
		}
		index := int64(store.target.path[len(view.storage.path)])
		if index < view.start || view.end >= 0 && index >= view.end {
			continue
		}
		key := pathKey(store.target.path)
		if visited[key] {
			continue
		}
		visited[key] = true
		location := address{root: view.storage.root, path: slices.Clone(store.target.path)}
		if origin := c.storedReference(location, point); origin != nil {
			destinations = append(destinations, c.decodeDestinations(origin, point, seen)...)
		}
	}
	return destinations
}
