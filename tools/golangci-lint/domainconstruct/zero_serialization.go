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

func (c *zeroChecker) decoderInvalidation(instruction ssa.CallInstruction, storage mutationTarget, t types.Type, before flowPoint, context flowContext) (zeroReason, *zeroProblem) {
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
					return reasonUncheckedError, nil
				}
				continue
			}
			for _, destination := range c.decodeDestinations(value, before, boundary.DefaultJSONDecode, make(map[ssa.Value]bool)) {
				target, known := c.collectionAddress(destination.value)
				if !known || target.root != storage.root || !storage.overlaps(target) {
					continue
				}
				if pointer, ok := destination.value.Type().Underlying().(*types.Pointer); ok {
					decoded := pointer.Elem()
					if boundary.DefaultJSONDecode && storage.elements != nil && sameStorage(target, storage.address) {
						// Default JSON decoding zeroes trailing slots when the
						// input is short, bypassing their element decoders.
						if array, ok := decoded.Underlying().(*types.Array); ok && array.Len() != 0 && !hasJSONDecoder(decoded) {
							return reasonInputMayBeZero, nil
						}
					}
					depth := len(target.path) - len(storage.path)
					if storage.elements == nil && depth != 0 {
						if depth < 0 && !types.Identical(decoded, storageType(target)) {
							return reasonInputMayBeZero, nil
						}
						continue
					}
					if storage.elements != nil {
						switch {
						case depth < 1:
							continue
						case depth > 1:
							path := target.path[len(storage.path):]
							if problem := c.decodedFieldProblem(t, path[1:], decoded, boundary.Codec, context.use); problem != nil {
								problem.path = fieldPath(elementLabel(int64(path[0])), problem.path)
								return problem.reason, problem
							}
							continue
						}
					}
					if c.decodeInvalidatesValue(decoded, t, boundary.Codec) {
						return reasonInputMayBeZero, nil
					}
				}
			}
		}
	}
	return "", nil
}

func (c *zeroChecker) decodeInvalidatesValue(decoded, expected types.Type, codec string) bool {
	_, scalar := decoded.Underlying().(*types.Basic)
	return (scalar || !types.Identical(decoded, expected)) && !c.decoderEstablishes(decoded, expected, codec)
}

// A checked write to a member of an element only affects that member's contract.
// Follow the same field selection as the use of the surrounding value.
func (c *zeroChecker) decodedFieldProblem(t types.Type, path []int, decoded types.Type, codec string, use valueUse) *zeroProblem {
	if !c.work.take() {
		return nil
	}
	if len(path) == 0 {
		if c.decodeInvalidatesValue(decoded, t, codec) {
			return c.failedResultProblem(t, reasonInputMayBeZero, use, make(map[types.Type]valueUse))
		}
		return nil
	}
	use = c.useForType(t, use)
	if array, ok := t.Underlying().(*types.Array); ok {
		index := int64(path[0])
		if array.Len() == 0 || index >= array.Len() {
			return nil
		}
		problem := c.decodedFieldProblem(array.Elem(), path[1:], decoded, codec, use)
		if problem != nil {
			problem.path = fieldPath(elementLabel(index), problem.path)
		}
		return problem
	}
	if _, structure := t.Underlying().(*types.Struct); !structure {
		return nil
	}
	for selected := range c.jsonShape.fields(t, use.json()) {
		if !c.work.take() {
			return nil
		}
		if pathPrefix(path, selected.indices) {
			problem := c.decodedFieldProblem(selected.field.Type(), path[len(selected.indices):], decoded, codec, use.forField(selected))
			if problem != nil {
				problem.path = fieldPath(selected.path, problem.path)
			}
			return problem
		}
	}
	return nil
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
		_, boxed := pointer.Elem().Underlying().(*types.Interface)
		_, pointerSlot := pointer.Elem().Underlying().(*types.Pointer)
		if defaultJSON && (boxed || pointerSlot) {
			if location, known := c.collectionAddress(value); known {
				origin, _ := c.referenceAt(location, point)
				for _, nested := range c.decodeDestinations(origin, point, defaultJSON, seen) {
					// JSON may follow a stored pointer or replace the slot itself,
					// for example when the input is null.
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
