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

// A view identifies a range in one backing allocation. Unknown ends are -1.
// The dynamic upper bound is relative to lengthBase in the backing storage.
type collectionView struct {
	storage      address
	element      types.Type
	start        int64
	end          int64
	capEnd       int64
	length       ssa.Value
	lengthBase   int64
	dynamicStart bool
	offset       ssa.Value // The offset of a single dynamically shifted view.
	value        ssa.Value
}

func (view collectionView) size() int64 {
	if view.end < 0 {
		return -1
	}
	return max(0, view.end-view.start)
}

// A dynamically shifted view has a length, but can reach any slot between
// its minimum start and the original backing capacity.
func (view collectionView) possibleEnd() int64 {
	if view.dynamicStart && view.size() != 0 {
		return view.capEnd
	}
	return view.end
}

type elementRange struct{ start, end int64 }

type collectionVisit struct {
	point   flowPoint
	root    ssa.Value
	path    string
	start   int64
	end     int64
	cleanup int
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
		if builtin.Name() != "append" {
			if view, ok := c.collectionOf(call.Args[0]); ok && boundedView(view, context).size() == 0 {
				return true
			}
		}
		if c.freshCollection(call.Args[0], make(map[ssa.Value]bool)) {
			return true
		}
		if builtin.Name() == "clear" {
			if slice, ok := call.Args[0].Type().Underlying().(*types.Slice); ok {
				if problem := c.zeroType(slice.Elem(), "", wholeValue); problem != nil {
					problem.reason = reasonCleared
					c.report(zeroUse{pos: instruction.Pos(), call: call}, problem, "clear", call.Args[0])
				}
			}
		} else {
			for index, argument := range call.Args[1:] {
				var problem *zeroProblem
				destination, destKnown := c.collectionOf(call.Args[0])
				source, sourceKnown := c.collectionOf(argument)
				if builtin.Name() == "copy" && destKnown && sourceKnown {
					destination, source = boundedView(destination, context), boundedView(source, context)
					definite := destination.size() >= 0 && source.size() >= 0
					source.end = endFromCount(source.start, minEnd(destination.size(), source.size()))
					problem = c.collectionStorageProblem(source, point, context, make(map[valueVisit]bool), nil, make(map[collectionVisit]bool))
					if !definite {
						problem = problem.uncertain()
					}
				} else {
					problem = c.problem(argument, nil, argument.Type(), point, context, make(map[valueVisit]bool))
				}
				if problem != nil {
					c.report(zeroUse{pos: instruction.Pos(), call: call, argument: index + 1}, problem, "store-element", argument)
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
		if call, ok := root.(*ssa.Call); ok {
			if before, appended, known := c.appendViews(&call.Call); known && before.end+appended.size() > before.capEnd {
				return true
			}
		}
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
	if !context.possible || seen[visit] || !c.work.take() {
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
		return alternatives.problem()
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
		if before, appended, ok := c.appendViews(&value.Call); ok {
			end := before.end + appended.size()
			if end > before.capEnd {
				return collectionView{storage: address{root: value}, element: before.element, end: before.size() + appended.size(), capEnd: -1, value: value}, true
			}
			before.end, before.length, before.value = end, nil, value
			return before, true
		}
		return c.sourceSliceView(value, 0, value)
	case *ssa.Extract:
		if call, ok := value.Tuple.(*ssa.Call); ok {
			return c.sourceSliceView(call, value.Index, value)
		}
	case *ssa.Alloc:
		if array, ok := value.Type().Underlying().(*types.Pointer).Elem().Underlying().(*types.Array); ok {
			return collectionView{storage: address{root: value}, element: array.Elem(), end: array.Len(), capEnd: array.Len(), value: value}, true
		}
	case *ssa.MakeSlice:
		length, known := integerValue(value.Len)
		if !known {
			length = -1
		}
		capacity, known := integerValue(value.Cap)
		if !known {
			capacity = -1
		}
		return collectionView{storage: address{root: value}, element: value.Type().Underlying().(*types.Slice).Elem(), end: length, capEnd: capacity, length: value.Len, value: value}, true
	case *ssa.Parameter, *ssa.FreeVar:
		switch t := value.Type().Underlying().(type) {
		case *types.Slice:
			return collectionView{storage: address{root: value}, element: t.Elem(), end: -1, capEnd: -1, value: value}, true
		case *types.Array:
			return collectionView{storage: address{root: value}, element: t.Elem(), end: t.Len(), capEnd: t.Len(), value: value}, true
		case *types.Pointer:
			if array, ok := t.Elem().Underlying().(*types.Array); ok {
				return collectionView{storage: address{root: value}, element: array.Elem(), end: array.Len(), capEnd: array.Len(), value: value}, true
			}
		}
	case *ssa.Slice:
		if view, ok := c.collectionOf(value.X); ok {
			low := int64(0)
			if value.Low != nil {
				var known bool
				low, known = integerValue(value.Low)
				if !known {
					if value.Low == value.High {
						view.end, view.length, view.value = view.start, nil, value
						if view.dynamicStart {
							view.offset = nil
						} else {
							view.offset = value.Low
						}
						view.dynamicStart = true
						return view, true
					}
					return collectionView{}, false
				}
			}
			if value.High != nil {
				if high, known := integerValue(value.High); known {
					view.end = view.start + high
					view.length = nil
				} else {
					view.end, view.length = -1, value.High
					view.lengthBase = view.start
				}
			}
			if value.Max != nil {
				if !view.dynamicStart {
					view.capEnd = -1
					if max, known := integerValue(value.Max); known {
						view.capEnd = view.start + max
					}
				}
			}
			view.start += low
			view.value = value
			return view, true
		}
	case *ssa.ChangeType:
		return c.collectionOf(value.X)
	case *ssa.Field:
		if origin := c.projectedReference(value.X, []int{value.Field}, make(map[valueVisit]bool), nil); origin != nil {
			return c.collectionOf(origin)
		}
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if array, ok := value.Type().Underlying().(*types.Array); ok {
				if storage, ok := addressOf(value.X); ok {
					return collectionView{storage: storage, element: array.Elem(), end: array.Len(), capEnd: array.Len(), value: value}, true
				}
			}
			if origin := localLoadOrigin(value); origin != nil {
				return c.collectionOf(origin)
			}
			if location, ok := c.collectionAddress(value.X); ok {
				if origin := c.storedReference(location, instructionPoint(value)); origin != nil {
					return c.collectionOf(origin)
				}
			}
		}
	case *ssa.FieldAddr:
		if array, ok := value.Type().Underlying().(*types.Pointer).Elem().Underlying().(*types.Array); ok {
			if storage, ok := addressOf(value); ok {
				return collectionView{storage: storage, element: array.Elem(), end: array.Len(), capEnd: array.Len(), value: value}, true
			}
		}
	}
	return collectionView{}, false
}

func (c *zeroChecker) collectionStorageProblem(view collectionView, point flowPoint, context flowContext, values map[valueVisit]bool, covered []elementRange, seen map[collectionVisit]bool) *zeroProblem {
	return c.collectionAtProblem(view, point, point, context, values, covered, seen)
}

func (c *zeroChecker) collectionAtProblem(view collectionView, point, use flowPoint, context flowContext, values map[valueVisit]bool, covered []elementRange, seen map[collectionVisit]bool) *zeroProblem {
	view = boundedView(view, context)
	if !context.possible || view.size() == 0 {
		return nil
	}
	if view.dynamicStart {
		view.end, view.length, view.dynamicStart = view.possibleEnd(), nil, false
		view.offset = nil
		return c.collectionAtProblem(view, point, use, context, values, covered, seen).uncertain()
	}
	if rangeCovered(covered, view.start, view.end) {
		return nil
	}
	visit := collectionVisit{point, view.storage.root, pathKey(view.storage.path), view.start, view.end, context.cleanup.first}
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
		if allocation, ok := instruction.(ssa.Value); ok && allocation == view.storage.root {
			if call, ok := allocation.(*ssa.Call); ok {
				if prefix, appended, known := c.appendViews(&call.Call); known {
					before := flowPoint{point.block, index}
					initial := intersectView(view, 0, prefix.size())
					if problem := c.copiedCollectionProblem(prefix, initial, 0, before, use, context, values, covered, seen); problem != nil {
						return problem
					}
					tail := intersectView(view, prefix.size(), -1)
					return c.copiedCollectionProblem(appended, tail, prefix.size(), before, use, context, values, covered, seen)
				}
			}
			problem := c.unfilledCollection(view, covered, reasonZeroStorage, context.use)
			if problem != nil && view.end < 0 {
				if source, _, _, known := c.rootSource(allocation); known && source.Kind == sourceChoice {
					problem = problem.uncertain()
				}
			}
			return problem.step("source", allocation.Pos(), "collection contains zero-initialized elements")
		}
		if store, ok := instruction.(*ssa.Store); ok {
			if destination, ok := c.collectionAddress(store.Addr); ok && destination.root == view.storage.root {
				if pathPrefix(view.storage.path, destination.path) {
					if sameStorage(view.storage, destination) {
						if source, known := c.collectionOf(store.Val); known {
							read := flowPoint{point.block, index}
							if load, ok := store.Val.(*ssa.UnOp); ok {
								read = instructionPoint(load)
							}
							return c.copiedCollectionProblem(source, view, 0, read, use, context, values, covered, seen).
								step("assignment", store.Pos(), "assign collection backing storage")
						}
					}
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
							if offset < 0 {
								problem = problem.uncertain()
							}
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
					if offset < 0 {
						before := flowPoint{point.block, index}
						return c.collectionAtProblem(view, before, use, context, values, covered, seen).
							afterUnknownMutation(store.Pos(), "indexed write", "collection elements")
					}
					covered = coverRange(covered, offset, offset+1)
				}
			}
		}
		if rangeCovered(covered, view.start, view.end) {
			return nil
		}
		before := flowPoint{point.block, index}
		switch instruction.(type) {
		case ssa.CallInstruction, *ssa.RunDefers:
		default:
			continue
		}
		for execution := range c.executedCallsAt(instruction, before, context) {
			call, before, context := execution.call, execution.before, execution.context
			common := call.Common()
			if builtin, ok := common.Value.(*ssa.Builtin); ok {
				if _, asynchronous := call.(*ssa.Go); asynchronous {
					continue
				}
				switch builtin.Name() {
				case "clear":
					if destination, ok := c.collectionOf(common.Args[0]); ok && sameStorage(destination.storage, view.storage) {
						destination = boundedView(destination, context)
						if destination.size() == 0 || !rangesOverlap(destination.start, destination.possibleEnd(), view.start, view.end) {
							continue
						}
						if problem := c.unfilledCollection(intersectView(view, destination.start, destination.possibleEnd()), covered, reasonCleared, context.use); problem != nil {
							if destination.end < 0 || destination.dynamicStart || execution.conditional {
								problem = problem.uncertain()
							}
							return problem.step("source", call.Pos(), "clear sets collection elements to zero")
						}
					}
				case "copy":
					if destination, ok := c.collectionOf(common.Args[0]); ok && sameStorage(destination.storage, view.storage) {
						if source, ok := c.collectionOf(common.Args[1]); ok {
							destination, source = boundedView(destination, context), boundedView(source, context)
							if destination.dynamicStart || source.dynamicStart {
								continue
							}
							transferred := intersectView(view, destination.start, endFromCount(destination.start, minEnd(destination.size(), source.size())))
							covers := !execution.conditional && copyCoversView(destination, source, view)
							definite := !execution.conditional && (destination.end >= 0 && source.end >= 0 || covers)
							if problem := c.copiedCollectionProblem(source, transferred, destination.start, before, use, context, values, covered, seen); problem != nil {
								if !definite {
									problem = problem.uncertain()
								}
								return problem
							}
							if covers {
								return nil
							}
							if definite && transferred.start < transferred.end {
								covered = coverRange(covered, transferred.start, transferred.end)
							}
						}
					}
				case "append":
					if destination, source, known := c.appendViews(common); known &&
						sameStorage(destination.storage, view.storage) && destination.end+source.size() <= destination.capEnd {
						transferred := intersectView(view, destination.end, destination.end+source.size())
						if problem := c.copiedCollectionProblem(source, transferred, destination.end, before, use, context, values, covered, seen); problem != nil {
							return problem
						}
						if transferred.start < transferred.end {
							covered = coverRange(covered, transferred.start, transferred.end)
						}
					}
				}
			} else {
				for _, remaining := range uncoveredViews(view, covered) {
					storage := mutationTarget{remaining.storage, &elementRange{remaining.start, remaining.end}}
					if reason := c.decoderInvalidation(call, storage, before, context); reason != "" {
						if problem := c.failedResultProblem(view.element, reason, context.use, make(map[types.Type]valueUse)); problem != nil {
							problem.path = fieldPath(elementLabel(remaining.start), problem.path)
							detail := " may leave partially decoded collection elements without checked success"
							if reason == reasonInputMayBeZero {
								detail = " decodes into a raw collection slot without establishing domain validity"
							}
							return problem.step("mutation", mutationPosition(call, before), c.callName(common)+detail)
						}
					}
					if c.collectionMutation(common, remaining, before) != mutationReadOnly {
						return c.collectionAtProblem(view, before, use, context, values, covered, seen).
							afterUnknownMutation(mutationPosition(call, before), c.callName(common), "collection elements")
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
		if c.work.exhausted() {
			return alternatives.problem().uncertain()
		}
		edge := c.edgeContext(context, point.block, index)
		if !edge.possible {
			continue
		}
		if filled, handoff := c.filledByLoop(view, predecessor, point.block, context.use, values); filled || handoff != nil {
			var problem *zeroProblem
			if handoff != nil {
				problem = c.unfilledCollection(view, covered, reasonInputMayBeZero, context.use).uncertain().
					step("mutation", handoff.Pos(), "shared backing storage may change during a channel handoff")
			}
			if alternatives.add(problem) {
				break
			}
			continue
		}
		if alternatives.add(c.collectionAtProblem(view, flowPoint{predecessor, len(predecessor.Instrs)}, use, edge, values, slices.Clone(covered), seen)) {
			break
		}
	}
	return alternatives.problem()
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
	if destination.start != 0 || source.start != 0 || destination.lengthBase != view.lengthBase {
		return false
	}
	length, ok := destination.length.(*ssa.Call)
	if !ok || len(length.Call.Args) != 1 || length.Call.Args[0] != source.value {
		return false
	}
	builtin, ok := length.Call.Value.(*ssa.Builtin)
	return ok && builtin.Name() == "len" && (destination.value == view.value || sameLength(destination.length, view.length))
}

// A scalar load can project one field of a copied element. Read the
// corresponding source slot at the copy, including overlapping copies.
func (c *zeroChecker) collectionWriteProblem(execution executedCall, location address, t types.Type,
	values map[valueVisit]bool, seen map[memoryVisit]bool) (problem *zeroProblem, definite, overlaps bool) {
	common := execution.call.Common()
	builtin, ok := common.Value.(*ssa.Builtin)
	if !ok || builtin.Name() != "clear" && builtin.Name() != "copy" {
		return nil, false, false
	}
	destination, ok := c.collectionOf(common.Args[0])
	base := len(destination.storage.path)
	if !ok || destination.storage.root != location.root || len(location.path) <= base || !pathPrefix(location.path, destination.storage.path) {
		return nil, false, false
	}
	context := execution.context
	destination = boundedView(destination, context)
	index := int64(location.path[base])
	end := minEnd(destination.possibleEnd(), destination.capEnd)
	if destination.size() == 0 || index < destination.start || end >= 0 && index >= end {
		return nil, false, false
	}
	_, asynchronous := execution.call.(*ssa.Go)
	definite = !execution.conditional && !asynchronous && !destination.dynamicStart && destination.end >= 0
	if builtin.Name() == "clear" {
		problem := c.zeroType(t, "", context.use)
		if problem != nil {
			problem.reason = reasonCleared
		}
		return problem.step("source", execution.call.Pos(), "clear sets collection elements to zero"), definite, true
	}

	source, ok := c.collectionOf(common.Args[1])
	if !ok {
		return nil, false, false
	}
	source = boundedView(source, context)
	if destination.dynamicStart || source.dynamicStart {
		return nil, false, false
	}
	offset := source.start + index - destination.start
	end = minEnd(source.end, source.capEnd)
	if end >= 0 && offset >= end {
		return nil, false, false
	}
	sourcePath := append(slices.Clone(source.storage.path), int(offset))
	source.storage.path = append(sourcePath, location.path[base+1:]...)
	before := execution.before
	problem = c.memoryAtProblem(source.storage, t, before, before, context, values, seen).
		step("assignment", execution.call.Pos(), "copy collection field")
	return problem, definite && source.end >= 0 && !c.work.exhausted(), true
}

func (c *zeroChecker) copiedCollectionProblem(source, destination collectionView, start int64, point, use flowPoint, context flowContext,
	values map[valueVisit]bool, covered []elementRange, seen map[collectionVisit]bool) *zeroProblem {
	for _, span := range uncoveredViews(destination, covered) {
		mapped := source
		mapped.start += span.start - start
		mapped.end = endFromCount(mapped.start, span.size())
		if problem := c.collectionAtProblem(mapped, point, use, context, values, nil, seen); problem != nil {
			if index, rest, ok := splitElementPath(problem.path); ok {
				problem.path = fieldPath(elementLabel(int64(index)-source.start+start), rest)
			}
			return problem
		}
	}
	return nil
}

func (c *zeroChecker) appendViews(call *ssa.CallCommon) (collectionView, collectionView, bool) {
	builtin, ok := call.Value.(*ssa.Builtin)
	if !ok || builtin.Name() != "append" || len(call.Args) != 2 {
		return collectionView{}, collectionView{}, false
	}
	before, beforeKnown := c.collectionOf(call.Args[0])
	added, addedKnown := c.collectionOf(call.Args[1])
	known := beforeKnown && addedKnown && !before.dynamicStart && !added.dynamicStart &&
		before.end >= 0 && before.capEnd >= 0 && added.end >= 0
	return before, added, known
}

// Only the transferred slots can change. An append that must allocate has
// an empty write range on the old backing storage.
func (c *zeroChecker) builtinWriteView(call *ssa.CallCommon) (collectionView, bool) {
	destination, known := c.collectionOf(call.Args[0])
	if !known {
		return collectionView{}, false
	}
	switch call.Value.(*ssa.Builtin).Name() {
	case "copy":
		if source, known := c.collectionOf(call.Args[1]); known {
			destination.end = endFromCount(destination.start, minEnd(destination.size(), source.size()))
		}
	case "append":
		if before, source, known := c.appendViews(call); known {
			destination.start = before.end
			destination.end = before.end + source.size()
			if destination.end > before.capEnd {
				destination.end = before.end
			}
		} else {
			if !destination.dynamicStart && destination.end >= 0 {
				destination.start = destination.end
			}
			destination.end = destination.capEnd
		}
	}
	return destination, true
}

func endFromCount(start, count int64) int64 {
	if count < 0 {
		return -1
	}
	return start + count
}

func boundedView(view collectionView, context flowContext) collectionView {
	if view.offset != nil {
		if offset, known := guardedInteger(view.offset, context); known && offset >= 0 {
			view.start += offset
			if view.end >= 0 {
				view.end += offset
			}
			view.lengthBase += offset
			view.dynamicStart, view.offset = false, nil
		}
	}
	if view.end < 0 && view.length != nil {
		if upper, known := guardedInteger(view.length, context); known {
			view.end = view.lengthBase + upper
		}
	}
	return view
}

func guardedInteger(target ssa.Value, context flowContext) (int64, bool) {
	for value, state := range context.facts {
		comparison, ok := value.(*ssa.BinOp)
		if !ok {
			continue
		}
		equal := comparison.Op == token.EQL && state == notZero || comparison.Op == token.NEQ && state == isZero
		if !equal {
			continue
		}
		for _, pair := range [][2]ssa.Value{{comparison.X, comparison.Y}, {comparison.Y, comparison.X}} {
			if pair[0] == target {
				if upper, known := integerValue(pair[1]); known {
					return upper, true
				}
			}
		}
	}
	return 0, false
}

func (c *zeroChecker) unfilledCollection(view collectionView, covered []elementRange, reason zeroReason, use valueUse) *zeroProblem {
	if rangeCovered(covered, view.start, view.end) {
		return nil
	}
	if problem := c.zeroType(view.element, "", use); problem != nil {
		problem.path = fieldPath(elementLabel(firstMissing(covered, view.start)), problem.path)
		problem.reason = reason
		return problem
	}
	return nil
}

func (c *zeroChecker) collectionAddress(value ssa.Value) (address, bool) {
	if change, ok := value.(*ssa.ChangeType); ok {
		return c.collectionAddress(change.X)
	}
	if field, ok := value.(*ssa.FieldAddr); ok {
		if storage, ok := c.collectionAddress(field.X); ok {
			storage.path = append(storage.path, field.Field)
			return storage, true
		}
	}
	if index, ok := value.(*ssa.IndexAddr); ok {
		if view, ok := c.collectionOf(index.X); ok {
			offset := -1
			if fixed, known := integerValue(index.Index); known && !view.dynamicStart {
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
