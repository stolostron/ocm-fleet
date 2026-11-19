package domainconstruct

import (
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

func (c *zeroChecker) resultSource(call *ssa.CallCommon, result int) (valueSource, bool) {
	if fact := c.callContract(call); fact != nil {
		for _, source := range fact.Sources {
			if source.Result == result {
				return source.Value, true
			}
		}
	}
	return valueSource{}, false
}

func (c *zeroChecker) callResultProblem(call *ssa.Call, result int, path []int, t types.Type, point flowPoint, context flowContext, seen map[valueVisit]bool) *zeroProblem {
	if source, ok := c.resultSource(&call.Call, result); ok {
		return c.sourceProblem(source, call, result, path, t, point, context, seen)
	}
	return nil
}

func (c *zeroChecker) sourceProblem(source valueSource, call *ssa.Call, result int, path []int, t types.Type, point flowPoint, context flowContext, seen map[valueVisit]bool) *zeroProblem {
	context.use = c.useForType(t, context.use)
	switch source.Kind {
	case sourceZero:
		return c.zeroType(t, "", context.use)
	case sourceParameter:
		// Arguments and receivers are checked at the call. Re-reporting their
		// origin through an identity or field accessor would duplicate that use.
		// A raw argument converted into a domain result has no such contract.
		if argument := contractArgument(&call.Call, source.Parameter); argument != nil {
			projection := append(append([]int(nil), source.Path...), path...)
			if original := projectedType(argument.Type(), projection); original != nil && !types.Identical(original, t) {
				return c.problem(argument, projection, t, point, context, seen)
			}
		}
		return nil
	case sourcePointer:
		if pointer, ok := t.Underlying().(*types.Pointer); ok {
			return c.memoryProblem(address{root: callResultValue(call, result), path: path}, pointer.Elem(), point, context, seen, make(map[memoryVisit]bool))
		}
	case sourceSlice:
		if view, ok := c.collectionOf(callResultValue(call, result)); ok {
			return c.collectionStorageProblem(view, point, context, seen, nil, make(map[collectionVisit]bool))
		}
	case sourceFields:
		if len(path) != 0 {
			if path[0] < len(source.Values) {
				return c.sourceProblem(source.Values[path[0]], call, result, path[1:], t, point, context, seen)
			}
			return nil
		}
		if payload := optionalPayload(t); payload != nil && len(source.Values) == 2 {
			// The canonical fields are value and present. A false flag leaves
			// the payload inert; a present or unknown flag requires valid T.
			// This also checks imported generic factories that call Some.
			if source.Values[1].Kind == sourceZero {
				return nil
			}
			return c.sourceProblem(source.Values[0], call, result, nil, payload, point, context, seen)
		}
		if structure, ok := t.Underlying().(*types.Struct); ok {
			for selected := range c.jsonShape.fields(structure, context.use.json()) {
				index := selected.indices[0]
				if index < len(source.Values) && !selected.indirect {
					if problem := c.sourceProblem(source.Values[index], call, result, selected.indices[1:], selected.field.Type(), point, context, seen); problem != nil {
						problem.path = fieldPath(selected.path, problem.path)
						return problem
					}
				}
			}
		}
	case sourceChoice:
		var alternatives zeroAlternatives
		for _, alternative := range source.Values {
			if alternatives.add(c.sourceProblem(alternative, call, result, path, t, point, context, seen)) {
				break
			}
		}
		return alternatives.problem().step("branch", call.Pos(), "generic helper selects an alternative result")
	case sourceConversion:
		if len(source.Values) != 0 {
			operand := source.Values[0]
			if basic, ok := t.Underlying().(*types.Basic); ok && basic.Info()&types.IsString != 0 &&
				operand.Kind == sourceZero && operand.ZeroKind == constant.Int {
				return nil // Integer zero converts to a one-rune string.
			}
			if problem, handled := c.narrowIntegerSource(operand, &call.Call, path, t, context.use); handled {
				return problem
			}
			return c.sourceProblem(operand, call, result, path, t, point, context, seen).
				step("conversion", call.Pos(), "generic helper converts raw representation to "+t.String())
		}
	}
	return nil
}

func (c *zeroChecker) narrowIntegerSource(source valueSource, call *ssa.CallCommon, path []int, target types.Type, use valueUse) (*zeroProblem, bool) {
	if source.Kind != sourceParameter || len(source.Path)+len(path) != 0 || c.pass.TypesSizes == nil {
		return nil, false
	}
	argument := contractArgument(call, source.Parameter)
	if argument == nil {
		return nil, false
	}
	from, integerFrom := argument.Type().Underlying().(*types.Basic)
	to, integerTo := target.Underlying().(*types.Basic)
	if !integerFrom || !integerTo || from.Info()&types.IsInteger == 0 || to.Info()&types.IsInteger == 0 {
		return nil, false
	}
	sizes := c.pass.TypesSizes
	if sizes.Sizeof(target) >= sizes.Sizeof(argument.Type()) {
		return nil, false
	}
	if literal, ok := argument.(*ssa.Const); ok && literal.Value != nil {
		one := constant.MakeInt64(1)
		width := uint(sizes.Sizeof(target) * 8)
		mask := constant.BinaryOp(constant.Shift(one, token.SHL, width), token.SUB, one)
		if constantZero(constant.BinaryOp(literal.Value, token.AND, mask)) {
			return c.zeroType(target, "", use), true
		}
		return nil, true
	}
	return c.conditionalProblem(target, reasonConversion, use), true
}

func (c *zeroChecker) sourceSliceView(call *ssa.Call, result int, value ssa.Value) (collectionView, bool) {
	source, ok := c.resultSource(&call.Call, result)
	if !ok {
		return collectionView{}, false
	}
	slice, ok := value.Type().Underlying().(*types.Slice)
	if !ok {
		return collectionView{}, false
	}
	return c.sliceSourceView(source, call, value, slice)
}

func (c *zeroChecker) sliceSourceView(source valueSource, call *ssa.Call, value ssa.Value, slice *types.Slice) (collectionView, bool) {
	view := collectionView{storage: address{root: value}, element: slice.Elem(), value: value}
	if source.Kind == sourceZero {
		return view, true
	}
	if source.Kind == sourceChoice {
		for index, alternative := range source.Values {
			child, ok := c.sliceSourceView(alternative, call, value, slice)
			if !ok {
				return collectionView{}, false
			}
			if index == 0 {
				view = child
				continue
			}
			if view.end < 0 || child.end < 0 {
				view.end = -1
			} else {
				view.end = max(view.end, child.end)
			}
			if !sameLength(view.length, child.length) {
				view.length = nil
			}
		}
		return view, true
	}
	if source.Kind != sourceSlice {
		return collectionView{}, false
	}
	view.capEnd = -1
	view.end = source.Length.Constant
	if source.Length.Parameter >= 0 {
		argument := contractArgument(&call.Call, source.Length.Parameter)
		if source.Length.FromLen {
			if original, ok := c.collectionOf(argument); ok {
				view.end, view.length = original.end-original.start, original.length
			}
		} else if length, known := integerValue(argument); known {
			view.end = length
		} else {
			view.end, view.length = -1, argument
		}
	}
	return view, true
}

func callResultValue(call *ssa.Call, result int) ssa.Value {
	if _, tuple := call.Type().(*types.Tuple); tuple {
		if references := call.Referrers(); references != nil {
			for _, reference := range *references {
				if extract, ok := reference.(*ssa.Extract); ok && extract.Index == result {
					return extract
				}
			}
		}
	}
	return call
}

func (c *zeroChecker) rootSource(root ssa.Value) (valueSource, *ssa.Call, int, bool) {
	switch root := root.(type) {
	case *ssa.Call:
		source, known := c.resultSource(&root.Call, 0)
		return source, root, 0, known
	case *ssa.Extract:
		if call, ok := root.Tuple.(*ssa.Call); ok {
			source, known := c.resultSource(&call.Call, root.Index)
			return source, call, root.Index, known
		}
	}
	return valueSource{}, nil, 0, false
}

func (c *zeroChecker) pointeeSourceProblem(source valueSource, call *ssa.Call, result int, path []int, t types.Type, point flowPoint, context flowContext, seen map[valueVisit]bool) *zeroProblem {
	if source.Kind == sourcePointer && len(source.Values) == 1 {
		return c.sourceProblem(source.Values[0], call, result, path, t, point, context, seen)
	}
	if source.Kind == sourceChoice {
		var alternatives zeroAlternatives
		for _, alternative := range source.Values {
			if alternatives.add(c.pointeeSourceProblem(alternative, call, result, path, t, point, context, seen)) {
				break
			}
		}
		return alternatives.problem().step("branch", call.Pos(), "generic helper selects an alternative result")
	}
	return nil
}

func freshSource(source valueSource) bool {
	switch source.Kind {
	case sourceZero, sourcePointer, sourceSlice:
		return true
	case sourceChoice:
		for _, alternative := range source.Values {
			if !freshSource(alternative) {
				return false
			}
		}
		return true
	}
	return false
}
