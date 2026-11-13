package domainconstruct

import (
	"cmp"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strconv"

	"golang.org/x/tools/go/ssa"
)

// A view identifies a range in one backing allocation. An unknown end is -1;
// length retains the expression needed to recognize complete range fills.
type collectionView struct {
	storage address
	element types.Type
	start   int64
	end     int64
	length  ssa.Value
	value   ssa.Value
}

type elementRange struct{ start, end int64 }

type collectionVisit struct {
	point flowPoint
	root  ssa.Value
	path  string
	start int64
	end   int64
}

func (c *zeroChecker) checkCollectionBuiltin(instruction ssa.CallInstruction, point flowPoint, context flowContext) bool {
	call := instruction.Common()
	builtin, ok := call.Value.(*ssa.Builtin)
	if !ok {
		return false
	}
	switch builtin.Name() {
	case "len", "cap":
		if c.ownRepresentation(call.Args[0].Type(), point) {
			return true
		}
		switch call.Args[0].Type().Underlying().(type) {
		case *types.Array, *types.Slice, *types.Map, *types.Chan:
			return true
		}
	case "append", "copy", "clear":
		if builtin.Name() == "clear" {
			if view, ok := c.collectionOf(call.Args[0]); ok && view.end >= 0 && view.start == view.end {
				return true
			}
		}
		if c.freshCollection(call.Args[0], make(map[ssa.Value]bool)) {
			return true
		}
		if builtin.Name() == "clear" {
			if slice, ok := call.Args[0].Type().Underlying().(*types.Slice); ok {
				if problem := c.zeroType(slice.Elem(), ""); problem != nil {
					problem.reason = reasonCleared
					c.report(zeroUse{pos: instruction.Pos(), call: call}, problem, "clear", call.Args[0])
				}
			}
		} else {
			for _, argument := range call.Args[1:] {
				if problem := c.problem(argument, nil, argument.Type(), point, context, make(map[valueVisit]bool)); problem != nil {
					c.report(zeroUse{pos: instruction.Pos(), call: call}, problem, "store-element", argument)
				}
			}
		}
		return true
	}
	return false
}

func (c *zeroChecker) freshCollection(value ssa.Value, seen map[ssa.Value]bool) bool {
	if seen[value] {
		return true
	}
	seen[value] = true
	if view, ok := c.collectionOf(value); ok {
		if c.freshStorage(view.storage.root) {
			return true
		}
	}
	switch value := value.(type) {
	case *ssa.Const:
		return value.IsNil()
	case *ssa.Phi:
		for _, incoming := range value.Edges {
			if !c.freshCollection(incoming, seen) {
				return false
			}
		}
		return true
	case *ssa.Call:
		if builtin, ok := value.Call.Value.(*ssa.Builtin); ok && builtin.Name() == "append" {
			return c.freshCollection(value.Call.Args[0], seen)
		}
	}
	return false
}

func (c *zeroChecker) freshStorage(root ssa.Value) bool {
	switch root := root.(type) {
	case *ssa.Alloc, *ssa.MakeSlice:
		return true
	case *ssa.Call, *ssa.Extract:
		source, _, _, known := c.rootSource(root)
		return known && freshSource(source)
	}
	return false
}

func (c *zeroChecker) collectionProblem(value ssa.Value, point flowPoint, context flowContext, seen map[valueVisit]bool) *zeroProblem {
	if view, ok := c.collectionOf(value); ok {
		return c.collectionStorageProblem(view, point, context, seen, nil, make(map[collectionVisit]bool))
	}
	if call, ok := value.(*ssa.Call); ok {
		if builtin, ok := call.Call.Value.(*ssa.Builtin); ok && builtin.Name() == "append" {
			for _, argument := range call.Call.Args {
				if problem := c.problem(argument, nil, argument.Type(), point, context, seen); problem != nil {
					return problem
				}
			}
		}
	}
	if slice, ok := value.(*ssa.Slice); ok {
		// A dynamic offset cannot hide a known invalid backing allocation.
		return c.problem(slice.X, nil, slice.X.Type(), point, context, seen)
	}
	return nil
}

func (c *zeroChecker) elementProblem(value, index ssa.Value, projection []int, t types.Type, point flowPoint, context flowContext, seen map[valueVisit]bool) *zeroProblem {
	read := point
	if load, ok := value.(*ssa.UnOp); ok && load.Op == token.MUL {
		if _, array := value.Type().Underlying().(*types.Array); array {
			read = instructionPoint(load)
		}
	}
	return c.elementAtProblem(value, index, projection, t, read, point, context, seen)
}

func (c *zeroChecker) elementAtProblem(value, index ssa.Value, projection []int, t types.Type, point, use flowPoint, context flowContext, seen map[valueVisit]bool) *zeroProblem {
	visit := valueVisit{value, pathKey(projection), t}
	if !context.possible || seen[visit] {
		return nil
	}
	seen[visit] = true
	defer delete(seen, visit)
	if view, ok := c.collectionOf(value); ok {
		if offset, known := integerValue(index); known {
			view.start += offset
			view.end = view.start + 1
		}
		// Element projections are checked after the containing element. A
		// constructor's successful element already carries its field contract.
		return c.collectionAtProblem(view, point, use, context, seen, nil, make(map[collectionVisit]bool))
	}
	if phi, ok := value.(*ssa.Phi); ok {
		var alternatives zeroAlternatives
		for position, incoming := range phi.Edges {
			edge := c.edgeContext(context, phi.Block(), position)
			if edge.possible {
				if alternatives.add(c.elementAtProblem(incoming, index, projection, t, point, use, edge, seen)) {
					break
				}
			}
		}
		return alternatives.problem(reasonBranch)
	}
	return c.problem(value, nil, value.Type(), use, context, seen)
}

func (c *zeroChecker) collectionOf(value ssa.Value) (collectionView, bool) {
	if c.locating[value] {
		return collectionView{}, false
	}
	c.locating[value] = true
	defer delete(c.locating, value)
	switch value := value.(type) {
	case *ssa.Const:
		if slice, ok := value.Type().Underlying().(*types.Slice); ok && value.IsNil() {
			return collectionView{storage: address{root: value}, element: slice.Elem(), value: value}, true
		}
	case *ssa.Call:
		return c.sourceSliceView(value, 0, value)
	case *ssa.Extract:
		if call, ok := value.Tuple.(*ssa.Call); ok {
			return c.sourceSliceView(call, value.Index, value)
		}
	case *ssa.Alloc:
		if array, ok := value.Type().Underlying().(*types.Pointer).Elem().Underlying().(*types.Array); ok {
			return collectionView{storage: address{root: value}, element: array.Elem(), end: array.Len(), value: value}, true
		}
	case *ssa.MakeSlice:
		length, known := integerValue(value.Len)
		if !known {
			length = -1
		}
		return collectionView{storage: address{root: value}, element: value.Type().Underlying().(*types.Slice).Elem(), end: length, length: value.Len, value: value}, true
	case *ssa.Parameter, *ssa.FreeVar:
		switch t := value.Type().Underlying().(type) {
		case *types.Slice:
			return collectionView{storage: address{root: value}, element: t.Elem(), end: -1, value: value}, true
		case *types.Array:
			return collectionView{storage: address{root: value}, element: t.Elem(), end: t.Len(), value: value}, true
		case *types.Pointer:
			if array, ok := t.Elem().Underlying().(*types.Array); ok {
				return collectionView{storage: address{root: value}, element: array.Elem(), end: array.Len(), value: value}, true
			}
		}
	case *ssa.Slice:
		if view, ok := c.collectionOf(value.X); ok {
			low := int64(0)
			if value.Low != nil {
				var known bool
				low, known = integerValue(value.Low)
				if !known {
					return collectionView{}, false
				}
			}
			if value.High != nil {
				if high, known := integerValue(value.High); known {
					view.end = view.start + high
					view.length = nil
				} else {
					view.end, view.length = -1, value.High
				}
			}
			view.start += low
			view.value = value
			return view, true
		}
	case *ssa.ChangeType:
		return c.collectionOf(value.X)
	case *ssa.Field:
		if origin := c.projectedReference(value.X, []int{value.Field}, make(map[valueVisit]bool), make(map[memoryVisit]bool)); origin != nil {
			return c.collectionOf(origin)
		}
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if array, ok := value.Type().Underlying().(*types.Array); ok {
				if storage, ok := addressOf(value.X); ok {
					return collectionView{storage: storage, element: array.Elem(), end: array.Len(), value: value}, true
				}
			}
			if origin := localLoadOrigin(value); origin != nil {
				return c.collectionOf(origin)
			}
			if location, ok := c.collectionAddress(value.X); ok {
				if origin := c.storedReference(location, instructionPoint(value), make(map[memoryVisit]bool)); origin != nil {
					return c.collectionOf(origin)
				}
			}
		}
	case *ssa.FieldAddr:
		if array, ok := value.Type().Underlying().(*types.Pointer).Elem().Underlying().(*types.Array); ok {
			if storage, ok := addressOf(value); ok {
				return collectionView{storage: storage, element: array.Elem(), end: array.Len(), value: value}, true
			}
		}
	}
	return collectionView{}, false
}

func (c *zeroChecker) collectionStorageProblem(view collectionView, point flowPoint, context flowContext, values map[valueVisit]bool, covered []elementRange, seen map[collectionVisit]bool) *zeroProblem {
	return c.collectionAtProblem(view, point, point, context, values, covered, seen)
}

func (c *zeroChecker) collectionAtProblem(view collectionView, point, use flowPoint, context flowContext, values map[valueVisit]bool, covered []elementRange, seen map[collectionVisit]bool) *zeroProblem {
	if !context.possible || view.end >= 0 && view.start >= view.end || rangeCovered(covered, view.start, view.end) {
		return nil
	}
	visit := collectionVisit{point, view.storage.root, pathKey(view.storage.path), view.start, view.end}
	if seen[visit] {
		return nil
	}
	seen[visit] = true
	defer delete(seen, visit)
	for index := point.index - 1; index >= 0; index-- {
		instruction := point.block.Instrs[index]
		if allocation, ok := instruction.(ssa.Value); ok && allocation == view.storage.root {
			return c.unfilledCollection(view, covered, reasonUninitialized).
				step("source", allocation.Pos(), "collection contains zero-initialized elements")
		}
		if store, ok := instruction.(*ssa.Store); ok {
			if destination, ok := c.collectionAddress(store.Addr); ok && destination.root == view.storage.root {
				if pathPrefix(view.storage.path, destination.path) {
					remaining := view.storage.path[len(destination.path):]
					array := types.NewArray(view.element, max(view.end, 0))
					return c.problem(store.Val, remaining, array, use, context, values).
						step("assignment", store.Pos(), "assign collection backing storage")
				}
				if pathPrefix(destination.path, view.storage.path) && len(destination.path) > len(view.storage.path) {
					offset := int64(destination.path[len(view.storage.path)])
					if offset >= 0 && (offset < view.start || view.end >= 0 && offset >= view.end || rangeCovered(covered, offset, offset+1)) {
						continue
					}
					remaining := destination.path[len(view.storage.path)+1:]
					if len(remaining) == 0 {
						if problem := c.problem(store.Val, nil, view.element, use, context, values); problem != nil {
							problem.path = fieldPath(elementLabel(offset), problem.path)
							return problem.step("assignment", store.Pos(), "assign collection element "+elementLabel(offset))
						}
					} else if offset >= 0 {
						storage := address{view.storage.root, append(slices.Clone(view.storage.path), int(offset))}
						if problem := c.memoryAtProblem(storage, view.element, point, use, context, values, make(map[memoryVisit]bool)); problem != nil {
							problem.path = fieldPath(elementLabel(offset), problem.path)
							return problem
						}
					}
					if offset >= 0 {
						covered = coverRange(covered, offset, offset+1)
					}
				}
			}
		}
		if call, ok := instruction.(ssa.CallInstruction); ok {
			common := call.Common()
			if builtin, ok := common.Value.(*ssa.Builtin); ok {
				switch builtin.Name() {
				case "clear":
					if destination, ok := c.collectionOf(common.Args[0]); ok && sameStorage(destination.storage, view.storage) && rangesOverlap(destination.start, destination.end, view.start, view.end) {
						if problem := c.unfilledCollection(intersectView(view, destination.start, destination.end), covered, reasonCleared); problem != nil {
							return problem
						}
					}
				case "copy":
					if destination, ok := c.collectionOf(common.Args[0]); ok && sameStorage(destination.storage, view.storage) {
						if source, ok := c.collectionOf(common.Args[1]); ok && copyCoversView(destination, source, view) {
							return c.collectionAtProblem(source, flowPoint{point.block, index}, use, context, values, nil, seen)
						}
						if source, ok := c.collectionOf(common.Args[1]); ok && destination.end >= 0 && source.end >= 0 {
							count := min(destination.end-destination.start, source.end-source.start)
							low, high := max(view.start, destination.start), minEnd(view.end, destination.start+count)
							if low < high && !rangeCovered(covered, low, high) {
								source.start += low - destination.start
								source.end = source.start + high - low
								if problem := c.collectionAtProblem(source, flowPoint{point.block, index}, use, context, values, nil, seen); problem != nil {
									return problem
								}
								covered = coverRange(covered, low, high)
							}
						}
					}
				}
			} else {
				before := flowPoint{point.block, index}
				for _, remaining := range uncoveredViews(view, covered) {
					storage := mutationTarget{remaining.storage, &elementRange{remaining.start, remaining.end}}
					if c.decoderMayInvalidate(call, storage, before, context) {
						if problem := c.failedResultProblem(view.element, reasonUncheckedError, make(map[types.Type]bool)); problem != nil {
							problem.path = fieldPath(elementLabel(remaining.start), problem.path)
							return problem.step("mutation", mutationPosition(call, before), callName(common)+" may leave partially decoded collection elements without checked success")
						}
					}
					if c.collectionMutation(common, remaining, before) != mutationReadOnly {
						return c.collectionAtProblem(view, before, use, context, values, covered, seen).
							uncertain(reasonMutation).
							step("mutation", mutationPosition(call, before), callName(common)+" may write collection elements")
					}
				}
			}
		}
		if rangeCovered(covered, view.start, view.end) {
			return nil
		}
	}
	var alternatives zeroAlternatives
	for index, predecessor := range point.block.Preds {
		edge := c.edgeContext(context, point.block, index)
		if !edge.possible {
			continue
		}
		if c.filledByLoop(view, predecessor, point.block, values) {
			if alternatives.add(nil) {
				break
			}
			continue
		}
		if alternatives.add(c.collectionAtProblem(view, flowPoint{predecessor, len(predecessor.Instrs)}, use, edge, values, slices.Clone(covered), seen)) {
			break
		}
	}
	return alternatives.problem(reasonBranch)
}

func uncoveredViews(view collectionView, covered []elementRange) []collectionView {
	var result []collectionView
	for _, span := range covered {
		if span.end <= view.start {
			continue
		}
		if span.start > view.start {
			child := intersectView(view, view.start, span.start)
			if child.end < 0 || child.start < child.end {
				result = append(result, child)
			}
		}
		view.start = max(view.start, span.end)
	}
	if view.end < 0 || view.start < view.end {
		result = append(result, view)
	}
	return result
}

func copyCoversView(destination, source, view collectionView) bool {
	if destination.start != 0 || source.start != 0 || view.start != 0 {
		return false
	}
	length, ok := destination.length.(*ssa.Call)
	if !ok || len(length.Call.Args) != 1 || length.Call.Args[0] != source.value {
		return false
	}
	builtin, ok := length.Call.Value.(*ssa.Builtin)
	return ok && builtin.Name() == "len" && sameLength(destination.length, view.length)
}

func (c *zeroChecker) unfilledCollection(view collectionView, covered []elementRange, reason zeroReason) *zeroProblem {
	if rangeCovered(covered, view.start, view.end) {
		return nil
	}
	if problem := c.zeroType(view.element, ""); problem != nil {
		problem.path = fieldPath(elementLabel(firstMissing(covered, view.start)), problem.path)
		problem.reason = reason
		return problem
	}
	return nil
}

func (c *zeroChecker) collectionAddress(value ssa.Value) (address, bool) {
	if field, ok := value.(*ssa.FieldAddr); ok {
		if storage, ok := c.collectionAddress(field.X); ok {
			storage.path = append(storage.path, field.Field)
			return storage, true
		}
	}
	if index, ok := value.(*ssa.IndexAddr); ok {
		if view, ok := c.collectionOf(index.X); ok {
			offset := -1
			if fixed, known := integerValue(index.Index); known {
				offset = int(view.start + fixed)
			}
			return address{root: view.storage.root, path: append(slices.Clone(view.storage.path), offset)}, true
		}
	}
	return addressOf(value)
}

func sameStorage(first, second address) bool {
	return first.root == second.root && slices.Equal(first.path, second.path)
}

func integerValue(value ssa.Value) (int64, bool) {
	if literal, ok := value.(*ssa.Const); ok && literal.Value != nil && literal.Value.Kind() == constant.Int {
		return constant.Int64Val(literal.Value)
	}
	return 0, false
}

func coverRange(covered []elementRange, start, end int64) []elementRange {
	covered = append(covered, elementRange{start, end})
	slices.SortFunc(covered, func(first, second elementRange) int { return cmp.Compare(first.start, second.start) })
	result := covered[:0]
	for _, span := range covered {
		if len(result) != 0 && result[len(result)-1].end >= span.start {
			result[len(result)-1].end = max(result[len(result)-1].end, span.end)
		} else {
			result = append(result, span)
		}
	}
	return result
}

func firstMissing(covered []elementRange, start int64) int64 {
	for _, span := range covered {
		if span.start > start {
			break
		}
		start = max(start, span.end)
	}
	return start
}

func rangeCovered(covered []elementRange, start, end int64) bool {
	return end >= 0 && firstMissing(covered, start) >= end
}

func minEnd(first, second int64) int64 {
	if first < 0 {
		return second
	}
	if second < 0 {
		return first
	}
	return min(first, second)
}

func rangesOverlap(firstStart, firstEnd, secondStart, secondEnd int64) bool {
	end := minEnd(firstEnd, secondEnd)
	return end < 0 || max(firstStart, secondStart) < end
}

func intersectView(view collectionView, start, end int64) collectionView {
	view.start, view.end = max(view.start, start), minEnd(view.end, end)
	return view
}

func elementLabel(index int64) string {
	if index < 0 {
		return "[index]"
	}
	return "[" + strconv.FormatInt(index, 10) + "]"
}
