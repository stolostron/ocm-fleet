package domainconstruct

import (
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"
)

func serializationCallObject(call *ssa.CallCommon) *types.Func {
	if call.IsInvoke() {
		return call.Method
	}
	if function := call.StaticCallee(); function != nil {
		object, _ := function.Object().(*types.Func)
		return object
	}
	return nil
}

func serializationArgument(call *ssa.CallCommon, index int) ssa.Value {
	if index == -1 && call.IsInvoke() {
		return call.Value
	}
	return contractArgument(call, index)
}

func serializationArgumentIndex(call *ssa.CallCommon, index int) int {
	if !call.IsInvoke() && call.Signature().Recv() != nil {
		return index + 1
	}
	return index
}

// Decoding accepts output storage. It does not consume its prior contents as a
// valid domain value; its success contract is considered separately below.
func (c *zeroChecker) decodingArgument(call *ssa.CallCommon, index int) bool {
	output := false
	for _, boundary := range c.contracts.boundariesFor(serializationCallObject(call), c.policies) {
		for _, input := range boundary.Encode {
			if serializationArgumentIndex(call, input) == index {
				return false // An input/output parameter consumes its prior state.
			}
		}
		for _, destination := range boundary.Decode {
			if serializationArgumentIndex(call, destination) == index {
				output = true
			}
		}
	}
	return output
}

// Only a declared root decoder establishes valid domain state on nil error.
// A plain envelope's nested hooks can be skipped for missing fields, so the
// envelope cannot provide the same guarantee. Failure can leave partial writes.
func (c *zeroChecker) decoderInitializes(instruction ssa.CallInstruction, location address, before flowPoint, context flowContext) bool {
	call := instruction.Common()
	for _, boundary := range c.contracts.boundariesFor(serializationCallObject(call), c.policies) {
		if len(boundary.Decode) == 0 || !c.decodingSucceeded(instruction, boundary.ErrorResult, context) {
			continue
		}
		for _, index := range boundary.Decode {
			argument := serializationArgument(call, index)
			for _, destination := range c.decodeDestinations(argument, before, make(map[ssa.Value]bool)) {
				pointer, ok := destination.Type().Underlying().(*types.Pointer)
				if !ok || !c.policies.lookup(pointer.Elem()).supports(boundary.Codec) {
					continue
				}
				if target, known := c.collectionAddress(destination); known && target.root == location.root && pathPrefix(location.path, target.path) {
					return true
				}
			}
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
	for _, boundary := range c.contracts.boundariesFor(serializationCallObject(call), c.policies) {
		if len(boundary.Decode) == 0 || c.decodingSucceeded(instruction, boundary.ErrorResult, context) {
			continue
		}
		for _, index := range boundary.Decode {
			if value := serializationArgument(call, index); value != nil && c.mayReferenceStorage(value, storage, before, make(map[ssa.Value]bool)) {
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
	case *ssa.ChangeType:
		return c.decodeDestinations(value.X, point, seen)
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if location, known := c.collectionAddress(value.X); known {
				if origin := c.storedReference(location, instructionPoint(value), make(map[memoryVisit]bool)); origin != nil {
					return c.decodeDestinations(origin, point, seen)
				}
			}
		}
	}
	if _, pointer := value.Type().Underlying().(*types.Pointer); pointer {
		return []ssa.Value{value}
	}
	view, ok := c.collectionOf(value)
	if !ok {
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
		if origin := c.storedReference(location, point, make(map[memoryVisit]bool)); origin != nil {
			destinations = append(destinations, c.decodeDestinations(origin, point, seen)...)
		}
	}
	return destinations
}
