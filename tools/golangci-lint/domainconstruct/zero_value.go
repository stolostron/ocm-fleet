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
	point flowPoint
	root  ssa.Value
	path  string
}

func (c *zeroChecker) problem(value ssa.Value, path []int, t types.Type, point flowPoint, context flowContext, seen map[valueVisit]bool) (result *zeroProblem) {
	if !context.possible || !c.interestingType(t) {
		return nil
	}
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
		result = result.withValue(c.valueName(value, path))
		switch value := value.(type) {
		case *ssa.ChangeType:
			result = result.step("conversion", value.Pos(), c.expressionAt(value.Pos())+" changes the type without validation")
		case *ssa.Convert:
			result = result.step("conversion", value.Pos(), c.expressionAt(value.Pos())+" changes the type without validation")
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
				return c.zeroType(t, "").step("source", value.Pos(), "value is zero on this path")
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
		return c.problem(value.X, path, value.Type(), point, context, seen)
	case *ssa.Slice, *ssa.MakeSlice:
		return c.collectionProblem(value, point, context, seen)
	case *ssa.Call:
		if problem := c.callResultProblem(value, 0, path, t, point, context, seen); problem != nil {
			return problem
		}
		if problem := c.rawValueProblem(value, path, t, point); problem != nil {
			return problem
		}
		if problem := c.pointerStorageProblem(value, path, t, point, context, seen); problem != nil {
			return problem
		}
		return c.collectionProblem(value, point, context, seen)
	case *ssa.Parameter, *ssa.FreeVar:
		if problem := c.rawValueProblem(value, path, t, point); problem != nil {
			return problem
		}
		return c.pointerStorageProblem(value, path, t, point, context, seen)
	case *ssa.Index:
		return c.elementProblem(value.X, value.Index, path, t, point, context, seen)
	case *ssa.Const:
		if zeroConstant(value) {
			return c.zeroType(t, "")
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
		return c.conditionalProblem(t, reasonConversion)
	case *ssa.ChangeInterface:
		return c.problem(value.X, path, value.X.Type(), point, context, seen)
	case *ssa.Field:
		return c.problem(value.X, append([]int{value.Field}, path...), t, point, context, seen)
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if index, ok := value.X.(*ssa.IndexAddr); ok {
				return c.elementAtProblem(index.X, index.Index, path, t, instructionPoint(value), point, context, seen)
			}
			if location, ok := addressOf(value.X); ok {
				location.path = append(location.path, path...)
				return c.memoryAtProblem(location, t, instructionPoint(value), point, context, seen, make(map[memoryVisit]bool))
			}
		}
		if value.Op == token.ARROW && c.zeroType(t, "") != nil {
			return c.conditionalProblem(t, reasonUncheckedReceive)
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
			return c.problem(assertion.X, path, t, point, context, seen)
		}
		if call, ok := value.Tuple.(*ssa.Call); ok {
			if problem := c.callResultProblem(call, value.Index, path, t, point, context, seen); problem != nil {
				return problem
			}
			if problem := c.rawValueProblem(value, path, t, point); problem != nil {
				return problem
			}
			return c.pointerStorageProblem(value, path, t, point, context, seen)
		}
	case *ssa.Lookup:
		return c.conditionalProblem(t, reasonUncheckedLookup)
	}
	return nil
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
func (c *zeroChecker) rawValueProblem(value ssa.Value, path []int, target types.Type, point flowPoint) *zeroProblem {
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
	name := c.valueName(value, path)
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
	detail := name + " " + entry + " as a plain " + typeName(source)
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
	return c.conditionalProblem(target, reason).withValue(name).step("source", value.Pos(), detail)
}

func projectedType(source types.Type, path []int) types.Type {
	for _, index := range path {
		structure, ok := source.Underlying().(*types.Struct)
		if !ok || index < 0 || index >= structure.NumFields() {
			return nil
		}
		source = structure.Field(index).Type()
	}
	return source
}

func (c *zeroChecker) tupleProblem(value *ssa.Extract, t types.Type, context flowContext) *zeroProblem {
	guardIndex, success, reason := -1, unknown, zeroReason("")
	switch tuple := value.Tuple.(type) {
	case *ssa.Call:
		results := tuple.Type().(*types.Tuple)
		for index := range results.Len() {
			if index != value.Index && isErrorType(results.At(index).Type()) {
				guardIndex, success, reason = index, isZero, reasonUncheckedError
				break
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
	if guardIndex < 0 {
		return nil
	}
	if references := value.Tuple.Referrers(); references != nil {
		for _, reference := range *references {
			if guard, ok := reference.(*ssa.Extract); ok && guard.Index == guardIndex {
				if c.valueTruth(guard, context, make(map[ssa.Value]bool)) == success {
					return nil
				}
			}
		}
	}
	if _, call := value.Tuple.(*ssa.Call); call {
		if problem := c.failedResultProblem(t, reason, make(map[types.Type]bool)); problem != nil {
			return problem
		}
		call := value.Tuple.(*ssa.Call)
		if fact := c.callContract(&call.Call); fact != nil {
			for _, rule := range fact.Payloads {
				if rule.Result == value.Index {
					for _, boxed := range rule.Types {
						if problem := c.payloadProblem(boxed, reason); problem != nil {
							return problem
						}
					}
				}
			}
		}
		return nil
	}
	return c.conditionalProblem(t, reason)
}

// Error payloads may contain a non-nil pointer to an invalid value. Optional
// absence does not make an unchecked failure payload valid.
func (c *zeroChecker) failedResultProblem(t types.Type, reason zeroReason, seen map[types.Type]bool) *zeroProblem {
	if seen[t] {
		return nil
	}
	seen[t] = true
	if problem := c.conditionalProblem(t, reason); problem != nil {
		return problem
	}
	switch t := t.Underlying().(type) {
	case *types.Pointer:
		return c.failedResultProblem(t.Elem(), reason, seen)
	case *types.Slice:
		return c.failedResultProblem(t.Elem(), reason, seen)
	case *types.Map:
		if problem := c.failedResultProblem(t.Key(), reason, seen); problem != nil {
			return problem
		}
		return c.failedResultProblem(t.Elem(), reason, seen)
	case *types.Struct:
		for index := range t.NumFields() {
			field := t.Field(index)
			if problem := c.failedResultProblem(field.Type(), reason, seen); problem != nil {
				problem.path = fieldPath(field.Name(), problem.path)
				return problem
			}
		}
	}
	return nil
}

func (c *zeroChecker) conditionalProblem(t types.Type, reason zeroReason) *zeroProblem {
	if problem := c.zeroType(t, ""); problem != nil {
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
	if c.validZeroStorage(location, t) {
		return nil
	}
	if array, ok := t.Underlying().(*types.Array); ok && c.policies.lookup(t) == nil {
		view := collectionView{storage: location, element: array.Elem(), end: array.Len()}
		return c.collectionAtProblem(view, point, use, context, values, nil, make(map[collectionVisit]bool))
	}
	if p := c.policies.lookup(t); p == nil || p.fact.ZeroValid {
		if structure, ok := t.Underlying().(*types.Struct); ok {
			for index := range structure.NumFields() {
				field := structure.Field(index)
				child := address{location.root, append(slices.Clone(location.path), index)}
				if problem := c.memoryAtProblem(child, field.Type(), point, use, context, values, seen); problem != nil {
					problem.path = fieldPath(field.Name(), problem.path)
					return problem
				}
			}
			return nil
		}
	}

	visit := memoryVisit{point, location.root, pathKey(location.path)}
	if seen[visit] {
		return nil
	}
	seen[visit] = true
	defer delete(seen, visit)
	for index := point.index - 1; index >= 0; index-- {
		instruction := point.block.Instrs[index]
		if allocation, ok := instruction.(ssa.Value); ok && allocation == location.root {
			if source, call, result, known := c.rootSource(allocation); known {
				return c.pointeeSourceProblem(source, call, result, location.path, t, use, context, values)
			}
			switch allocation.(type) {
			case *ssa.Call, *ssa.Extract:
				return nil // Opaque pointer factories carry their declared contract.
			}
			problem := c.zeroType(t, "")
			if problem != nil {
				problem.reason = reasonZeroStorage
			}
			return problem.withValue(projectedName(location.root, location.path)).
				step("source", allocation.Pos(), "storage "+projectedName(location.root, location.path)+" starts at zero")
		}
		if store, ok := instruction.(*ssa.Store); ok {
			if destination, ok := c.collectionAddress(store.Addr); ok && destination.root == location.root {
				if pathPrefix(location.path, destination.path) {
					remaining := location.path[len(destination.path):]
					return c.problem(store.Val, remaining, t, use, context, values).
						step("assignment", store.Pos(), "assign to "+projectedName(location.root, location.path))
				}
				// A populated field makes the containing protected value nonzero.
				// Its constructor remains responsible for all other invariants.
				if pathPrefix(destination.path, location.path) {
					if literal, constant := store.Val.(*ssa.Const); !constant || !zeroConstant(literal) {
						return nil
					}
				}
			}
		}
		if call, ok := instruction.(ssa.CallInstruction); ok {
			before := flowPoint{point.block, index}
			pos := mutationPosition(call, before)
			if c.decoderInitializes(call, location, before, context) {
				return nil
			}
			if c.decoderMayInvalidate(call, mutationTarget{address: location}, before, context) {
				return c.failedResultProblem(t, reasonUncheckedError, make(map[types.Type]bool)).
					withValue(projectedName(location.root, location.path)).
					step("mutation", pos, callName(call.Common())+" may leave partially decoded state without checked success")
			}
			switch c.callMutation(call.Common(), location, before) {
			case mutationZero:
				return c.zeroType(t, "").withValue(projectedName(location.root, location.path)).step("source", pos, "helper writes zero").
					step("mutation", pos, "definite zero write by "+callName(call.Common()))
			case mutationNonzero:
				return nil
			case mutationUnknown:
				if c.iteratorInitializes(call.Common(), location, t, before, context) {
					return nil
				}
				prior := c.memoryAtProblem(location, t, before, use, context, values, seen)
				if prior == nil && !storageHasDomainContract(location, t) {
					prior = c.conditionalProblem(t, reasonInputMayBeZero).step("source", location.root.Pos(), "raw storage "+projectedName(location.root, location.path)+" has no domain construction contract")
				}
				return prior.afterUnknownMutation(pos, callName(call.Common()), projectedName(location.root, location.path))
			}
		}
	}
	var alternatives zeroAlternatives
	for index, predecessor := range point.block.Preds {
		edge := c.edgeContext(context, point.block, index)
		if !edge.possible {
			continue
		}
		end := flowPoint{predecessor, len(predecessor.Instrs)}
		if alternatives.add(c.memoryAtProblem(location, t, end, use, edge, values, seen)) {
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

func projectedName(value ssa.Value, path []int) string {
	name := value.Name()
	if allocation, ok := value.(*ssa.Alloc); ok && allocation.Comment != "" {
		name = allocation.Comment
	}
	return projectName(name, value.Type(), path)
}

func projectName(name string, t types.Type, path []int) string {
	if pointer, ok := t.Underlying().(*types.Pointer); ok {
		t = pointer.Elem()
	}
	for _, index := range path {
		switch parent := t.Underlying().(type) {
		case *types.Struct:
			if index < 0 || index >= parent.NumFields() {
				return name + pathKey(path)
			}
			field := parent.Field(index)
			name, t = name+"."+field.Name(), field.Type()
		case *types.Array:
			name, t = name+elementLabel(int64(index)), parent.Elem()
		case *types.Slice:
			name, t = name+elementLabel(int64(index)), parent.Elem()
		default:
			return name + pathKey(path)
		}
	}
	return name
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

func callName(call *ssa.CallCommon) string {
	if function := call.StaticCallee(); function != nil {
		return function.Name()
	}
	if call.Method != nil {
		return call.Method.Name()
	}
	return call.Value.Name()
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
			for index := range t.NumFields() {
				interesting = c.containsDomainType(t.Field(index).Type(), seen) || interesting
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
