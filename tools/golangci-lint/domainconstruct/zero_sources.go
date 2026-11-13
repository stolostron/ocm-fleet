package domainconstruct

import (
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

func sourceArgument(call *ssa.CallCommon, index int) ssa.Value {
	if index == -1 {
		if function := call.StaticCallee(); function != nil && function.Signature.Recv() != nil && !call.IsInvoke() {
			return call.Args[0]
		}
		return nil
	}
	return contractArgument(call, index)
}

func (c *zeroChecker) callResultProblem(call *ssa.Call, result int, path []int, t types.Type, point flowPoint, context flowContext, seen map[valueVisit]bool) *zeroProblem {
	if source, ok := c.resultSource(&call.Call, result); ok {
		return c.sourceProblem(source, call, result, path, t, point, context, seen)
	}
	return nil
}

func (c *zeroChecker) sourceProblem(source valueSource, call *ssa.Call, result int, path []int, t types.Type, point flowPoint, context flowContext, seen map[valueVisit]bool) *zeroProblem {
	switch source.Kind {
	case sourceZero:
		return c.zeroType(t, "")
	case sourceParameter:
		// Arguments and receivers are checked at the call. Re-reporting their
		// origin through an identity or field accessor would duplicate that use.
		// A raw argument converted into a domain result has no such contract.
		if argument := sourceArgument(&call.Call, source.Parameter); argument != nil {
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
		if structure, ok := t.Underlying().(*types.Struct); ok {
			for index, child := range source.Values {
				if index < structure.NumFields() {
					field := structure.Field(index)
					if problem := c.sourceProblem(child, call, result, nil, field.Type(), point, context, seen); problem != nil {
						problem.path = fieldPath(field.Name(), problem.path)
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
			return c.sourceProblem(source.Values[0], call, result, path, t, point, context, seen).
				step("conversion", call.Pos(), "generic helper converts raw representation to "+t.String())
		}
	}
	return nil
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
	view.end = source.Length.Constant
	if source.Length.Parameter >= 0 {
		argument := sourceArgument(&call.Call, source.Length.Parameter)
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
