package domainconstruct

import (
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ssa"
)

type valueVisit struct {
	value ssa.Value
	path  string
	typ   types.Type
}

type address struct {
	root ssa.Value
	path []int
}

type memoryVisit struct {
	point   flowPoint
	root    ssa.Value
	path    string
	cleanup int
}

func (c *zeroChecker) problem(value ssa.Value, path []int, t types.Type, point flowPoint, context flowContext, seen map[valueVisit]bool) (result *zeroProblem) {
	if !context.possible || !c.interestingType(t) || !c.work.take() {
		return nil
	}
	context.use = c.useForType(t, context.use)
	visit := valueVisit{value, pathKey(path), t}
	if seen[visit] {
		return nil
	}
	seen[visit] = true
	defer delete(seen, visit)
	defer func() {
		if result == nil {
			return
		}
		if result.value == "" {
			result = result.withValue(c.valueName(value, path))
		}
		switch value := value.(type) {
		case *ssa.ChangeType, *ssa.Convert:
			result = result.step("conversion", value.Pos(), c.valueReference(value, nil).describe("conversion")+" changes the type without validation")
		case *ssa.Field:
			field := value.X.Type().Underlying().(*types.Struct).Field(value.Field)
			result = result.step("field", value.Pos(), "read field "+field.Name())
		}
	}()
	if _, rawScalar := value.Type().Underlying().(*types.Basic); rawScalar && len(path) == 0 {
		if _, basic := t.Underlying().(*types.Basic); basic {
			switch c.valueTruth(value, context, make(map[ssa.Value]bool)) {
			case notZero:
				return nil
			case isZero:
				return c.zeroType(t, "", context.use).step("source", value.Pos(), "value is zero on this path")
			}
		}
	}

	switch value := value.(type) {
	case *ssa.MakeInterface:
		return c.problem(value.X, path, value.X.Type(), point, context, seen)
	case *ssa.MakeClosure:
		if function, ok := value.Fn.(*ssa.Function); ok && strings.HasPrefix(function.Synthetic, "bound method wrapper") {
			for _, binding := range value.Bindings {
				if problem := c.problem(binding, nil, binding.Type(), point, context, seen); problem != nil {
					return problem
				}
			}
		}
	case *ssa.TypeAssert:
		return c.assertedProblem(value, path, t, point, context, seen)
	case *ssa.Slice:
		if basic, ok := value.Type().Underlying().(*types.Basic); ok && basic.Info()&types.IsString != 0 {
			if length, known := stringSliceLength(value); known {
				if length == 0 {
					return c.zeroType(t, "", context.use).step("source", value.Pos(), "slice bounds produce an empty string")
				}
				return nil
			}
		}
		return c.collectionProblem(value, point, context, seen)
	case *ssa.MakeSlice:
		return c.collectionProblem(value, point, context, seen)
	case *ssa.Call:
		if problem := c.callResultProblem(value, 0, path, t, point, context, seen); problem != nil {
			return problem
		}
		if problem := c.rawValueProblem(value, path, t, point, context.use); problem != nil {
			return problem
		}
		if problem := c.pointerStorageProblem(value, path, t, point, context, seen); problem != nil {
			return problem
		}
		return c.collectionProblem(value, point, context, seen)
	case *ssa.Parameter, *ssa.FreeVar:
		if problem := c.rawValueProblem(value, path, t, point, context.use); problem != nil {
			return problem
		}
		return c.pointerStorageProblem(value, path, t, point, context, seen)
	case *ssa.Index:
		return c.elementProblem(value.X, value.Index, path, t, point, context, seen)
	case *ssa.Const:
		if zeroConstant(value) {
			return c.zeroType(t, "", context.use)
		}
		return nil
	case *ssa.Phi:
		var alternatives zeroAlternatives
		for index, incoming := range value.Edges {
			edge := c.edgeContext(context, value.Block(), index)
			if !edge.possible {
				continue
			}
			if alternatives.add(c.problem(incoming, path, t, point, edge, seen)) {
				break
			}
		}
		return alternatives.problem().step("branch", value.Pos(), "merge values from incoming branches")
	case *ssa.ChangeType:
		return c.problem(value.X, path, t, point, context, seen)
	case *ssa.Convert:
		return c.conditionalProblem(t, reasonConversion, context.use)
	case *ssa.ChangeInterface:
		return c.problem(value.X, path, value.X.Type(), point, context, seen)
	case *ssa.Field:
		return c.problem(value.X, append([]int{value.Field}, path...), t, point, context, seen)
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if index, ok := value.X.(*ssa.IndexAddr); ok {
				return c.elementAtProblem(index.X, index.Index, path, t, instructionPoint(value), point, context, seen)
			}
			if location, ok := c.collectionAddress(value.X); ok {
				location.path = append(location.path, path...)
				return c.memoryAtProblem(location, t, instructionPoint(value), point, context, seen, make(map[memoryVisit]bool))
			}
		}
		if value.Op == token.ARROW {
			return c.conditionalProblem(t, reasonUncheckedReceive, context.use)
		}
	case *ssa.Alloc, *ssa.FieldAddr, *ssa.IndexAddr:
		if index, ok := value.(*ssa.IndexAddr); ok {
			if pointer, ok := t.Underlying().(*types.Pointer); ok {
				return c.elementProblem(index.X, index.Index, path, pointer.Elem(), point, context, seen)
			}
		}
		if location, ok := c.collectionAddress(value); ok {
			if pointer, ok := t.Underlying().(*types.Pointer); ok {
				location.path = append(location.path, path...)
				return c.memoryProblem(location, pointer.Elem(), point, context, seen, make(map[memoryVisit]bool))
			}
		}
	case *ssa.Extract:
		if result := c.tupleProblem(value, t, context); result != nil {
			return result
		}
		if assertion, ok := value.Tuple.(*ssa.TypeAssert); ok && value.Index == 0 {
			return c.assertedProblem(assertion, path, t, point, context, seen)
		}
		if _, lookup := value.Tuple.(*ssa.Lookup); lookup && value.Index == 0 {
			return c.rawValueProblem(value, path, t, point, context.use)
		}
		if call, ok := value.Tuple.(*ssa.Call); ok {
			if problem := c.callResultProblem(call, value.Index, path, t, point, context, seen); problem != nil {
				return problem
			}
			if problem := c.rawValueProblem(value, path, t, point, context.use); problem != nil {
				return problem
			}
			return c.pointerStorageProblem(value, path, t, point, context, seen)
		}
	case *ssa.Lookup:
		return c.conditionalProblem(t, reasonUncheckedLookup, context.use)
	}
	return nil
}

func (c *zeroChecker) assertedProblem(assertion *ssa.TypeAssert, path []int, t types.Type, point flowPoint, context flowContext, seen map[valueVisit]bool) *zeroProblem {
	boxed := assertion.X
	for {
		change, ok := boxed.(*ssa.ChangeInterface)
		if !ok || !c.work.take() {
			break
		}
		boxed = change.X
	}
	if concrete, ok := boxed.(*ssa.MakeInterface); ok && types.Identical(concrete.X.Type(), assertion.AssertedType) {
		return c.problem(concrete.X, path, t, point, context, seen)
	}
	if !assertion.CommaOk {
		t = assertion.AssertedType
	}
	return c.problem(assertion.X, path, t, point, context, seen)
}

// A pointer's construction contract describes its initial contents. Later
// decoding can invalidate those contents even when the pointer is unchanged.
func (c *zeroChecker) pointerStorageProblem(value ssa.Value, path []int, t types.Type, point flowPoint, context flowContext, seen map[valueVisit]bool) *zeroProblem {
	pointer, ok := t.Underlying().(*types.Pointer)
	if !ok || len(path) != 0 {
		return nil
	}
	if location, ok := c.collectionAddress(value); ok {
		return c.memoryProblem(location, pointer.Elem(), point, context, seen, make(map[memoryVisit]bool))
	}
	return nil
}

// Domain-typed inputs carry a validity contract. Raw data only acquires that
// contract through construction, even when a conversion gives it a domain name.
func (c *zeroChecker) rawValueProblem(value ssa.Value, path []int, target types.Type, point flowPoint, use valueUse) *zeroProblem {
	source := projectedType(value.Type(), path)
	if source == nil {
		return nil
	}
	if _, boxed := source.Underlying().(*types.Interface); boxed {
		return nil // A successful assertion preserves the asserted type's contract.
	}
	if types.Identical(source, target) {
		return nil
	}
	// A collection read can describe a prefix of a whole-array copy with a
	// synthetic shorter array type. Its elements retain their domain contract;
	// this does not authorize a conversion into a protected array type.
	if targetArray, ok := target.(*types.Array); ok && c.policies.lookup(source) == nil {
		if sourceArray, ok := source.Underlying().(*types.Array); ok &&
			targetArray.Len() <= sourceArray.Len() && types.Identical(sourceArray.Elem(), targetArray.Elem()) {
			return nil
		}
	}
	reference := c.valueReference(value, path)
	reason := reasonInputMayBeZero
	entry := "enters this function"
	switch value := value.(type) {
	case *ssa.Parameter:
		if value.Parent().Signature.Recv() != nil && value.Parent().Params[0] == value {
			entry = "enters this method"
		}
	case *ssa.Call, *ssa.Extract:
		entry = "is returned"
	case *ssa.FreeVar:
		entry = "is captured"
	}
	detail := reference.describe("value") + " " + entry + " as a plain " + typeName(source)
	if p := c.policies.lookup(source); p != nil {
		if !p.fact.ZeroValid {
			return nil
		}
		reason = reasonSourcePermitsZero
		detail = typeName(source) + " permits zero; conversion does not establish a nonzero " + typeName(target)
	}
	if p := c.policies.lookup(target); p != nil {
		if function, ok := point.block.Parent().Object().(*types.Func); ok && p.functions[function.Origin()] {
			return nil // Validation and compatibility are this constructor's contract.
		}
	}
	return c.conditionalProblem(target, reason, use).withValue(reference.expression).step("source", value.Pos(), detail)
}

func projectedType(source types.Type, path []int) types.Type {
	for _, index := range path {
		if pointer, ok := source.Underlying().(*types.Pointer); ok {
			source = pointer.Elem()
		}
		structure, ok := source.Underlying().(*types.Struct)
		if !ok || index < 0 || index >= structure.NumFields() {
			return nil
		}
		source = structure.Field(index).Type()
	}
	return source
}

func stringSliceLength(value *ssa.Slice) (int64, bool) {
	if value.High == nil {
		return 0, false
	}
	if value.High == value.Low {
		return 0, true
	}
	high, known := integerValue(value.High)
	if !known || value.Low == nil {
		return high, known
	}
	low, known := integerValue(value.Low)
	return high - low, known
}

func (c *zeroChecker) tupleProblem(value *ssa.Extract, t types.Type, context flowContext) *zeroProblem {
	guardIndex, success, reason := -1, unknown, zeroReason("")
	switch tuple := value.Tuple.(type) {
	case *ssa.Call:
		results := tuple.Type().(*types.Tuple)
		for index := range results.Len() {
			if index != value.Index && c.errorStatus(results.At(index).Type()) &&
				!c.tupleGuardChecked(tuple, index, isZero, context) {
				return c.uncheckedCallResult(tuple, value.Index, t, reasonUncheckedError, context.use)
			}
		}
	case *ssa.Lookup:
		if value.Index == 0 {
			guardIndex, success, reason = 1, notZero, reasonUncheckedLookup
		}
	case *ssa.UnOp:
		if tuple.Op == token.ARROW && value.Index == 0 {
			guardIndex, success, reason = 1, notZero, reasonUncheckedReceive
		}
	case *ssa.TypeAssert:
		if value.Index == 0 {
			guardIndex, success, reason = 1, notZero, reasonUncheckedAssert
		}
	case *ssa.Select:
		if value.Index >= 2 {
			guardIndex, success, reason = 1, notZero, reasonUncheckedReceive
		}
	}
	if call, ok := value.Tuple.(*ssa.Call); ok {
		if value.Index == 0 && c.contracts.hasPresence(callObject(&call.Call)) &&
			!c.tupleGuardChecked(value.Tuple, 1, notZero, context) {
			if optionalGet(callObject(&call.Call)) {
				// Unlike an arbitrary presence API's inactive payload, Get
				// returns exactly zero T on absence. Nil pointers and empty
				// collections therefore retain their ordinary valid zeros.
				return c.conditionalProblem(t, reasonUncheckedPresence, context.use)
			}
			return c.uncheckedCallResult(call, value.Index, t, reasonUncheckedPresence, context.use)
		}
		return nil
	}
	if guardIndex < 0 || c.tupleGuardChecked(value.Tuple, guardIndex, success, context) {
		return nil
	}
	return c.conditionalProblem(t, reason, context.use)
}

func (c *zeroChecker) tupleGuardChecked(tuple ssa.Value, index int, success truth, context flowContext) bool {
	if references := tuple.Referrers(); references != nil {
		for _, reference := range *references {
			if guard, ok := reference.(*ssa.Extract); ok && guard.Index == index {
				if c.valueTruth(guard, context, make(map[ssa.Value]bool)) == success {
					return true
				}
				if success == isZero && isErrorType(guard.Type()) && c.errorGuardChecked(guard, context) {
					return true
				}
			}
		}
	}
	return false
}

func (c *zeroChecker) uncheckedCallResult(call *ssa.Call, result int, t types.Type, reason zeroReason, use valueUse) *zeroProblem {
	if problem := c.failedResultProblem(t, reason, use, make(map[types.Type]valueUse)); problem != nil {
		return problem
	}
	if fact := c.callContract(&call.Call); fact != nil {
		for _, rule := range fact.Payloads {
			if rule.Result == result {
				for _, boxed := range rule.Types {
					if boxed.SuccessOnly {
						continue
					}
					if problem := c.payloadProblem(boxed, reason, use); problem != nil {
						return problem
					}
				}
			}
		}
	}
	return nil
}

// Inert failure or absence payloads may contain a non-nil pointer to an invalid
// value. Optional absence does not make an unchecked payload valid.
func (c *zeroChecker) failedResultProblem(t types.Type, reason zeroReason, use valueUse, seen map[types.Type]valueUse) *zeroProblem {
	use = c.useForType(t, use)
	// A custom marshaler can require a whole-value check of a type whose
	// JSON fields were already inspected elsewhere in the same result.
	if prior, checked := seen[t]; checked && (prior == wholeValue || prior == use) {
		return nil
	}
	seen[t] = use
	if problem := c.conditionalProblem(t, reason, use); problem != nil {
		return problem
	}
	switch shape := t.Underlying().(type) {
	case *types.Pointer:
		return c.failedResultProblem(shape.Elem(), reason, use, seen)
	case *types.Slice:
		return c.failedResultProblem(shape.Elem(), reason, use, seen)
	case *types.Array:
		if shape.Len() != 0 {
			if problem := c.failedResultProblem(shape.Elem(), reason, use, seen); problem != nil {
				problem.path = fieldPath("[0]", problem.path)
				return problem
			}
		}
	case *types.Map:
		if problem := c.failedResultProblem(shape.Key(), reason, wholeValue, seen); problem != nil {
			return problem
		}
		return c.failedResultProblem(shape.Elem(), reason, use, seen)
	case *types.Struct:
		for selected := range c.jsonShape.fields(t, use.json()) {
			if problem := c.failedResultProblem(selected.field.Type(), reason, use, seen); problem != nil {
				problem.path = fieldPath(selected.path, problem.path)
				return problem
			}
		}
	}
	return nil
}

func (c *zeroChecker) conditionalProblem(t types.Type, reason zeroReason, use valueUse) *zeroProblem {
	if problem := c.zeroType(t, "", use); problem != nil {
		problem.reason, problem.possible = reason, true
		return problem
	}
	return nil
}

func (c *zeroChecker) memoryProblem(location address, t types.Type, point flowPoint, context flowContext, values map[valueVisit]bool, seen map[memoryVisit]bool) *zeroProblem {
	return c.memoryAtProblem(location, t, point, point, context, values, seen)
}

// A copied value is read at point. Its referenced storage is observed at use,
// so copying a struct or slice header does not freeze its backing contents.
func (c *zeroChecker) memoryAtProblem(location address, t types.Type, point, use flowPoint, context flowContext, values map[valueVisit]bool, seen map[memoryVisit]bool) *zeroProblem {
	if !context.possible || !c.interestingType(t) {
		return nil
	}
	if c.storageReadFact(location, t, point, context) == notZero {
		return nil
	}
	if c.validZeroStorage(location, t) {
		return nil
	}
	context.use = c.useForType(t, context.use)
	if optionalPayload(t) != nil {
		// Follow whole-value writes and generic factories, without inspecting
		// the inactive private payload as an independently required field.
		return c.memoryHistoryProblem(location, t, point, use, context, values, seen)
	}
	if array, ok := t.Underlying().(*types.Array); ok && c.policies.lookup(t) == nil {
		view := collectionView{storage: location, element: array.Elem(), end: array.Len()}
		return c.collectionAtProblem(view, point, use, context, values, nil, make(map[collectionVisit]bool))
	}
	if p := c.policies.lookup(t); p == nil || p.fact.ZeroValid {
		if structure, ok := t.Underlying().(*types.Struct); ok {
			for selected := range c.jsonShape.fields(t, context.use.json()) {
				if child, read, known := c.fieldStorage(location, structure, selected, point, use); known {
					childContext := context
					if context.use.json() && selected.indirect {
						childContext.use = jsonAddressableFields
					}
					if problem := c.memoryAtProblem(child, selected.field.Type(), read, use, childContext, values, seen); problem != nil {
						problem.path = fieldPath(selected.path, problem.path)
						return problem
					}
				}
			}
			return nil
		}
	}
	return c.memoryHistoryProblem(location, t, point, use, context, values, seen)
}

func (c *zeroChecker) fieldStorage(location address, shape *types.Struct, field structField, point, use flowPoint) (address, flowPoint, bool) {
	if !field.indirect {
		location.path = append(slices.Clone(location.path), field.indices...)
		return location, point, true
	}
	location.path = slices.Clone(location.path)
	for _, index := range field.indices[:len(field.indices)-1] {
		location.path = append(location.path, index)
		t := shape.Field(index).Type()
		if pointer, ok := t.Underlying().(*types.Pointer); ok {
			origin := c.storedReference(location, point)
			root, known := c.collectionAddress(origin)
			if !known {
				return address{}, flowPoint{}, false
			}
			location, point, t = root, use, pointer.Elem()
		}
		shape = t.Underlying().(*types.Struct)
	}
	location.path = append(location.path, field.indices[len(field.indices)-1])
	return location, point, true
}

// Guard facts describe the observation above. Once an intervening effect rules
// out that proof, tracing earlier storage must not reapply it before the effect.
func (c *zeroChecker) memoryHistoryProblem(location address, t types.Type, point, use flowPoint, context flowContext, values map[valueVisit]bool, seen map[memoryVisit]bool) *zeroProblem {
	visit := memoryVisit{point, location.root, pathKey(location.path), context.cleanup.first}
	if seen[visit] || !c.work.take() {
		return nil
	}
	seen[visit] = true
	defer delete(seen, visit)
	for index := point.index - 1; index >= 0; index-- {
		if !c.work.take() {
			return nil
		}
		instruction := point.block.Instrs[index]
		if allocation, ok := instruction.(ssa.Value); ok && allocation == location.root {
			if source, call, result, known := c.rootSource(allocation); known {
				return c.pointeeSourceProblem(source, call, result, location.path, t, use, context, values)
			}
			switch allocation.(type) {
			case *ssa.Call, *ssa.Extract:
				return nil // Opaque pointer factories carry their declared contract.
			}
			problem := c.zeroType(t, "", context.use)
			if problem == nil {
				return nil
			}
			problem.reason = reasonZeroStorage
			reference := c.valueReference(location.root, location.path)
			return problem.withValue(reference.expression).
				step("source", allocation.Pos(), "storage "+reference.describe("unnamed storage")+" starts at zero")
		}
		before := flowPoint{point.block, index}
		if store, ok := instruction.(*ssa.Store); ok {
			destination, known := c.collectionAddress(store.Addr)
			if known && destination.root == location.root {
				if pathPrefix(location.path, destination.path) {
					remaining := location.path[len(destination.path):]
					problem := c.problem(store.Val, remaining, t, use, context, values)
					if problem == nil {
						return nil
					}
					return problem.step("assignment", store.Pos(), "assign to "+c.storageName(location.root, location.path))
				}
				// A populated field makes the containing protected value nonzero.
				// Its constructor remains responsible for all other invariants.
				if pathPrefix(destination.path, location.path) {
					if literal, constant := store.Val.(*ssa.Const); !constant || !zeroConstant(literal) {
						return nil
					}
				}
			}
			if known && borrowedScalarMayAlias(location, destination, context) {
				detail := "write through a pointer parameter that may alias " + c.storageName(location.root, location.path)
				if problem := c.problem(store.Val, nil, t, before, context, values); problem != nil {
					return problem.uncertain().step("mutation", store.Pos(), detail)
				}
				return c.memoryHistoryProblem(location, t, before, use, context, values, seen).uncertain().
					step("mutation", store.Pos(), detail)
			}
		}
		switch instruction.(type) {
		case ssa.CallInstruction, *ssa.RunDefers:
		default:
			continue
		}
		for execution := range c.executedCallsAt(instruction, before, context) {
			call, before, context := execution.call, execution.before, execution.context
			_, asynchronous := call.(*ssa.Go)
			conditional := execution.conditional || asynchronous
			pos := mutationPosition(call, before)
			if !conditional && c.decoderInitializes(call, location, before, context) {
				return nil
			}
			if reason := c.decoderInvalidation(call, mutationTarget{address: location}, before, context); reason != "" {
				problem := c.failedResultProblem(t, reason, context.use, make(map[types.Type]valueUse))
				if problem == nil {
					problem = c.memoryHistoryProblem(location, t, before, use, context, values, seen).uncertain()
				}
				detail := " may leave partially decoded state without checked success"
				if reason == reasonInputMayBeZero {
					detail = " decodes into raw storage without establishing domain validity"
				}
				return problem.withValue(c.valueName(location.root, location.path)).
					step("mutation", pos, c.callName(call.Common())+detail)
			}
			if problem, definite, overlaps := c.collectionWriteProblem(execution, location, t, values, seen); overlaps {
				if definite {
					return problem
				}
				if problem != nil {
					return problem.uncertain()
				}
				return c.memoryHistoryProblem(location, t, before, use, context, values, seen).uncertain()
			}
			effect := c.callMutation(call.Common(), location, before)
			if conditional && effect != mutationReadOnly {
				// A reachable single defer has no other cleanup that could repair
				// its zero write. Keep that write as a possible outcome.
				if effect != mutationZero || !execution.conditional || len(c.functionDefers(call.Parent())) != 1 {
					effect = mutationUnknown
				}
			}
			switch effect {
			case mutationZero:
				problem := c.zeroType(t, "", context.use).withValue(c.valueName(location.root, location.path)).step("source", pos, "helper writes zero")
				if conditional {
					if problem == nil {
						continue
					}
					return problem.uncertain().step("mutation", pos, "conditional zero write by "+c.callName(call.Common()))
				}
				return problem.step("mutation", pos, "definite zero write by "+c.callName(call.Common()))
			case mutationNonzero:
				return nil
			case mutationUnknown:
				if _, synchronous := call.(*ssa.Call); synchronous && c.iteratorInitializes(call.Common(), location, t, before, context) {
					return nil
				}
				prior := c.memoryHistoryProblem(location, t, before, use, context, values, seen)
				if prior == nil && !storageHasDomainContract(location, t) {
					prior = c.conditionalProblem(t, reasonInputMayBeZero, context.use).step("source", location.root.Pos(), "raw storage "+c.storageName(location.root, location.path)+" has no domain construction contract")
				}
				if prior == nil {
					return nil
				}
				return prior.afterUnknownMutation(pos, c.callName(call.Common()), c.storageName(location.root, location.path))
			}
		}
	}
	var alternatives zeroAlternatives
	for index, predecessor := range point.block.Preds {
		if c.work.exhausted() {
			return alternatives.problem().uncertain()
		}
		edge := c.edgeContext(context, point.block, index)
		if !edge.possible {
			continue
		}
		end := flowPoint{predecessor, len(predecessor.Instrs)}
		if alternatives.add(c.memoryHistoryProblem(location, t, end, use, edge, values, seen)) {
			break
		}
	}
	return alternatives.problem()
}

func mutationPosition(call ssa.CallInstruction, point flowPoint) token.Pos {
	if pos := call.Pos(); pos.IsValid() {
		return pos
	}
	for _, argument := range call.Common().Args {
		if closure, ok := argument.(*ssa.MakeClosure); ok {
			if function, ok := closure.Fn.(*ssa.Function); ok && function.Synthetic == "range-over-func yield" {
				return function.Pos()
			}
		}
	}
	return instructionPosition(call, point)
}

func storageHasDomainContract(location address, target types.Type) bool {
	pointer, ok := location.root.Type().Underlying().(*types.Pointer)
	if !ok {
		return false
	}
	t := pointer.Elem()
	for _, index := range location.path {
		switch parent := t.Underlying().(type) {
		case *types.Struct:
			if index < 0 || index >= parent.NumFields() {
				return false
			}
			t = parent.Field(index).Type()
		case *types.Array:
			t = parent.Elem()
		default:
			return false
		}
	}
	return types.Identical(t, target)
}

func (c *zeroChecker) interestingType(t types.Type) bool {
	if interesting, found := c.interesting[t]; found {
		return interesting
	}
	interesting := c.containsDomainType(t, make(map[types.Type]bool))
	c.interesting[t] = interesting
	return interesting
}

func (c *zeroChecker) containsDomainType(t types.Type, seen map[types.Type]bool) bool {
	if interesting, found := c.interesting[t]; found {
		return interesting
	}
	if seen[t] {
		return false
	}
	seen[t] = true
	interesting := false
	if p := c.policies.lookup(t); p != nil && !p.fact.ZeroValid {
		interesting = true
	} else {
		switch t := t.Underlying().(type) {
		case *types.Interface, *types.Signature:
			interesting = true
		case *types.Pointer:
			interesting = c.containsDomainType(t.Elem(), seen)
		case *types.Slice:
			interesting = c.containsDomainType(t.Elem(), seen)
		case *types.Array:
			interesting = c.containsDomainType(t.Elem(), seen)
		case *types.Map:
			interesting = c.containsDomainType(t.Key(), seen) || c.containsDomainType(t.Elem(), seen)
		case *types.Struct:
			for field := range t.Fields() {
				interesting = c.containsDomainType(field.Type(), seen) || interesting
			}
		}
	}
	return interesting
}

func addressOf(value ssa.Value) (address, bool) {
	switch value := value.(type) {
	case *ssa.Call, *ssa.Extract:
		if _, pointer := value.Type().Underlying().(*types.Pointer); pointer {
			return address{root: value}, true
		}
	case *ssa.Alloc, *ssa.MakeSlice, *ssa.Parameter, *ssa.FreeVar, *ssa.Global:
		return address{root: value}, true
	case *ssa.FieldAddr:
		if location, ok := addressOf(value.X); ok {
			location.path = append(location.path, value.Field)
			return location, true
		}
	case *ssa.ChangeType:
		return addressOf(value.X)
	case *ssa.IndexAddr:
		if index, ok := value.Index.(*ssa.Const); ok && index.Value != nil {
			if location, ok := addressOf(value.X); ok {
				location.path = append(location.path, int(index.Int64()))
				return location, true
			}
		}
	}
	return address{}, false
}

func instructionPoint(instruction ssa.Instruction) flowPoint {
	block := instruction.Block()
	for index, candidate := range block.Instrs {
		if candidate == instruction {
			return flowPoint{block, index}
		}
	}
	return flowPoint{block, len(block.Instrs)}
}

func pathPrefix(path, prefix []int) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

func pathKey(path []int) string {
	var text strings.Builder
	for _, index := range path {
		text.WriteByte('.')
		text.WriteString(strconv.Itoa(index))
	}
	return text.String()
}
