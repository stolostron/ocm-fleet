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
				if boundary.DefaultJSONEncode {
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
	if use.json() && c.policies.lookup(t) != nil {
		return wholeValue
	}
	return jsonUseForType(t, use)
}

func jsonUseForType(t types.Type, use valueUse) valueUse {
	if !use.json() {
		return use
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

func (use valueUse) forField(field structField) valueUse {
	if use.json() && field.indirect {
		return jsonAddressableFields
	}
	return use
}

type decodeDestination struct {
	value       ssa.Value // nil when a slot could not be recovered.
	conditional bool
}

// Only a declared root decoder establishes valid domain state on nil error.
// A plain envelope's nested hooks can be skipped for missing fields, so the
// envelope cannot provide the same guarantee. Failure can leave partial writes.
func (c *zeroChecker) decoderInitializes(instruction ssa.CallInstruction, location address, t types.Type, before flowPoint, context flowContext) bool {
	call := instruction.Common()
	for _, boundary := range c.contracts.boundariesFor(callObject(call), c.policies) {
		if len(boundary.Decode) == 0 || !c.decodingSucceeded(instruction, boundary.ErrorResult, context) {
			continue
		}
		initialized, conflicting := false, false
		for _, index := range boundary.Decode {
			argument := contractArgument(call, index)
			for _, destination := range c.decodeDestinations(argument, before, boundary.DefaultJSONDecode, make(map[ssa.Value]bool)) {
				target, known := c.collectionAddress(destination.value)
				if !known {
					conflicting = true
					continue
				}
				if !(mutationTarget{address: location}).overlaps(target) {
					continue
				}
				pointer, ok := destination.value.Type().Underlying().(*types.Pointer)
				if !ok || target.root != location.root || !pathPrefix(location.path, target.path) ||
					slices.Contains(target.path, -1) {
					conflicting = true
					continue
				}
				expected := t
				if len(target.path) < len(location.path) {
					// A native parent decoder can establish the declared policy
					// of its required fields.
					if !types.Identical(storageType(location), t) {
						conflicting = true
						continue
					}
					expected = storageType(target)
				}
				if !c.decoderEstablishes(pointer.Elem(), expected, boundary.Codec) {
					conflicting = true
				} else if !destination.conditional {
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

func (c *zeroChecker) decoderEstablishes(decoded, expected types.Type, codec string) bool {
	return types.Identical(decoded, expected) &&
		c.policies.lookup(decoded).supports(codec) && supportsNativeDecode(decoded, codec)
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

func (c *zeroChecker) decoderInvalidation(instruction ssa.CallInstruction, storage mutationTarget, t types.Type, before flowPoint, context flowContext) zeroReason {
	call := instruction.Common()
	for _, boundary := range c.contracts.boundariesFor(callObject(call), c.policies) {
		if len(boundary.Decode) == 0 {
			continue
		}
		succeeded := c.decodingSucceeded(instruction, boundary.ErrorResult, context)
		for _, index := range boundary.Decode {
			value := contractArgument(call, index)
			if value == nil {
				continue
			}
			if !succeeded {
				if c.mayReferenceStorage(value, storage, before, make(map[ssa.Value]bool)) {
					return reasonUncheckedError
				}
				continue
			}
			for _, destination := range c.decodeDestinations(value, before, boundary.DefaultJSONDecode, make(map[ssa.Value]bool)) {
				target, known := c.collectionAddress(destination.value)
				if !known || target.root != storage.root || !storage.overlaps(target) || slices.Contains(target.path, -1) {
					continue
				}
				if pointer, ok := destination.value.Type().Underlying().(*types.Pointer); ok {
					decoded := pointer.Elem()
					if boundary.DefaultJSONDecode && storage.elements != nil && sameStorage(target, storage.address) {
						// Default JSON decoding zeroes trailing slots when the
						// input is short, bypassing their element decoders.
						if array, ok := decoded.Underlying().(*types.Array); ok && array.Len() != 0 && !hasJSONDecoder(decoded) {
							return reasonInputMayBeZero
						}
					}
					if storage.elements == nil && !sameStorage(target, storage.address) {
						if pathPrefix(storage.path, target.path) && !types.Identical(decoded, storageType(target)) {
							return reasonInputMayBeZero
						}
						continue
					}
					// A sibling field of a collection element cannot invalidate
					// another field's domain contract.
					if storage.elements != nil && len(target.path) != len(storage.path)+1 {
						continue
					}
					if _, scalar := decoded.Underlying().(*types.Basic); (scalar || !types.Identical(decoded, t)) &&
						!c.decoderEstablishes(decoded, t, boundary.Codec) {
						return reasonInputMayBeZero
					}
				}
			}
		}
	}
	return ""
}

// Recover decode slots from local boxing and variadic storage. An unknown or
// conditional output can conflict with a definite write but cannot prove one.
func (c *zeroChecker) decodeDestinations(value ssa.Value, point flowPoint, defaultJSON bool, seen map[ssa.Value]bool) []decodeDestination {
	if value == nil || seen[value] || !c.work.take() {
		return []decodeDestination{{}}
	}
	seen[value] = true
	defer delete(seen, value)
	switch value := value.(type) {
	case *ssa.Const:
		if value.IsNil() {
			return nil
		}
	case *ssa.MakeInterface:
		return c.decodeDestinations(value.X, point, defaultJSON, seen)
	case *ssa.ChangeInterface:
		return c.decodeDestinations(value.X, point, defaultJSON, seen)
	case *ssa.Phi:
		var destinations []decodeDestination
		agree := true
		for _, edge := range value.Edges {
			incoming := c.decodeDestinations(edge, point, defaultJSON, seen)
			if len(incoming) != 1 || incoming[0].conditional || incoming[0].value == nil {
				agree = false
			} else if agree && len(destinations) != 0 {
				agree = c.sameDecodeDestination(destinations[0].value, incoming[0].value)
			}
			if c.work.exhausted() {
				return []decodeDestination{{}}
			}
			destinations = append(destinations, incoming...)
		}
		if agree && len(destinations) != 0 {
			return destinations[:1]
		}
		for index := range destinations {
			destinations[index].conditional = true
		}
		return destinations
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if location, known := c.collectionAddress(value.X); known {
				if origin := c.storedReference(location, instructionPoint(value)); origin != nil {
					return c.decodeDestinations(origin, point, defaultJSON, seen)
				}
			}
		}
	}
	if pointer, ok := value.Type().Underlying().(*types.Pointer); ok {
		destinations := []decodeDestination{{value: value}}
		if _, boxed := pointer.Elem().Underlying().(*types.Interface); defaultJSON && boxed {
			if location, known := c.collectionAddress(value); known {
				origin, _ := c.referenceAt(location, point)
				for _, nested := range c.decodeDestinations(origin, point, defaultJSON, seen) {
					// JSON can follow the stored pointer, or replace the interface
					// itself, for example when the input is null.
					nested.conditional = true
					destinations = append(destinations, nested)
				}
			}
		}
		return destinations
	}
	if defaultJSON {
		return nil // An interface payload that is not a pointer is replaced.
	}
	view, ok := c.collectionOf(value)
	if !ok || view.end < 0 || view.dynamicStart {
		return []decodeDestination{{}}
	}
	var destinations []decodeDestination
	visited := make(map[int64]bool)
	for _, store := range c.stores[view.storage.root] {
		if !c.work.take() {
			return []decodeDestination{{}}
		}
		if !pathPrefix(store.target.path, view.storage.path) || len(store.target.path) != len(view.storage.path)+1 {
			continue
		}
		index := int64(store.target.path[len(view.storage.path)])
		if index < view.start || view.end >= 0 && index >= view.end {
			continue
		}
		if visited[index] {
			continue
		}
		visited[index] = true
		location := address{root: view.storage.root, path: slices.Clone(store.target.path)}
		destinations = append(destinations, c.decodeDestinations(c.storedReference(location, point), point, defaultJSON, seen)...)
	}
	if int64(len(visited)) != view.size() {
		destinations = append(destinations, decodeDestination{})
	}
	return destinations
}

func (c *zeroChecker) sameDecodeDestination(first, second ssa.Value) bool {
	if !types.Identical(first.Type(), second.Type()) {
		return false
	}
	left, leftKnown := c.collectionAddress(first)
	right, rightKnown := c.collectionAddress(second)
	return leftKnown && rightKnown && sameStorage(left, right) && !slices.Contains(left.path, -1)
}
