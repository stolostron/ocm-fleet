package domainconstruct

import (
	"cmp"
	"go/constant"
	"go/token"
	"go/types"
	"iter"
	"slices"
	"strconv"

	"golang.org/x/exp/typeparams"
	"golang.org/x/tools/go/ssa"
)

// A view identifies a range in one backing allocation. Unknown ends are -1.
// When summarized alternatives have different lengths, end is the possible
// end and shortestEnd is the guaranteed end. A symbolic length is relative
// to lengthBase in the backing storage.
type collectionView struct {
	storage      address
	element      types.Type
	start        int64
	end          int64
	shortestEnd  *int64
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

func (view collectionView) guaranteedSize() int64 {
	if view.shortestEnd == nil {
		return view.size()
	}
	if *view.shortestEnd < 0 {
		return -1
	}
	return max(0, minEnd(view.end, *view.shortestEnd)-view.start)
}

func (view collectionView) guaranteedEnd() int64 {
	return endFromCount(view.start, view.guaranteedSize())
}

func (view *collectionView) setLengthBounds(shortest, longest int64) {
	view.end, view.shortestEnd = endFromCount(view.start, longest), nil
	if shortest != longest {
		end := endFromCount(view.start, shortest)
		view.shortestEnd = &end
	}
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
			if view, ok := c.collectionOfIn(call.Args[0], context); ok && boundedView(view, context).size() == 0 {
				return true
			}
		}
		if c.freshCollection(call.Args[0], make(map[ssa.Value]bool)) {
			return true
		}
		if builtin.Name() == "clear" {
			if slice, ok := collectionOperandShape(call.Args[0].Type()).(*types.Slice); ok {
				if problem := c.zeroType(slice.Elem(), "", wholeValue); problem != nil {
					problem.reason = reasonCleared
					c.report(zeroUse{pos: instruction.Pos(), call: call}, problem, "clear", call.Args[0])
				}
			}
		} else {
			for index, argument := range call.Args[1:] {
				var problem *zeroProblem
				destination, destKnown := c.collectionOfIn(call.Args[0], context)
				source, sourceKnown := c.collectionOfIn(argument, context)
				if builtin.Name() == "copy" && destKnown && sourceKnown {
					destination, source = boundedView(destination, context), boundedView(source, context)
					count := minEnd(destination.size(), source.size())
					definite := count >= 0 && copyGuaranteedEnd(destination, source)-destination.start >= count
					source.end = endFromCount(source.start, count)
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
			if before, appended, known := c.appendViews(&call.Call, flowContext{}); known && before.end+appended.size() > before.capEnd {
				return true
			}
		}
		source, _, _, known := c.rootSource(root)
		return known && freshSource(source)
	}
	return false
}

func (c *zeroChecker) collectionProblem(value ssa.Value, point flowPoint, context flowContext, seen map[valueVisit]bool) *zeroProblem {
	if view, ok := c.collectionOfIn(value, context); ok {
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
	if view, ok := c.collectionOfIn(value, context); ok {
		view = boundedView(view, context)
		if offset, known := integerValue(index); known {
			view.start += offset
			view.end, view.shortestEnd = view.start+1, nil
			if !view.dynamicStart && (len(projection) != 0 || !types.Identical(t, view.element)) {
				// Inspect the requested field or converted type at this slot.
				// The collection's element type may allow a zero representation.
				location := view.storage
				location.path = append(slices.Clone(location.path), int(view.start))
				location.path = append(location.path, projection...)
				return c.memoryAtProblem(location, t, point, use, context, seen, make(map[memoryVisit]bool))
			}
		}
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
	return c.collectionOfIn(value, flowContext{})
}

func (c *zeroChecker) collectionOfIn(value ssa.Value, context flowContext) (collectionView, bool) {
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
		if before, appended, ok := c.appendViews(&value.Call, context); ok {
			shortest, longest := before.size()+appended.guaranteedSize(), before.size()+appended.size()
			if before.end+appended.size() > before.capEnd {
				result := collectionView{storage: address{root: value}, element: before.element, capEnd: -1, value: value}
				result.setLengthBounds(shortest, longest)
				return result, true
			}
			before.setLengthBounds(shortest, longest)
			before.length, before.value = nil, value
			return before, true
		}
		return c.sourceSliceView(value, 0, value, context)
	case *ssa.Extract:
		if call, ok := value.Tuple.(*ssa.Call); ok {
			return c.sourceSliceView(call, value.Index, value, context)
		}
	case *ssa.Alloc:
		if array, ok := value.Type().Underlying().(*types.Pointer).Elem().Underlying().(*types.Array); ok {
			return collectionView{storage: address{root: value}, element: array.Elem(), end: array.Len(), capEnd: array.Len(), value: value}, true
		}
	case *ssa.MakeSlice:
		slice, ok := collectionOperandShape(value.Type()).(*types.Slice)
		if !ok {
			return collectionView{}, false
		}
		length, known := integerValue(value.Len)
		if !known {
			length = -1
		}
		capacity, known := integerValue(value.Cap)
		if !known {
			capacity = -1
		}
		return collectionView{storage: address{root: value}, element: slice.Elem(), end: length, capEnd: capacity, length: value.Len, value: value}, true
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
	case *ssa.SliceToArrayPointer:
		pointer, ok := collectionOperandShape(value.Type()).(*types.Pointer)
		if !ok {
			return collectionView{}, false
		}
		array, ok := pointer.Elem().Underlying().(*types.Array)
		if !ok {
			return collectionView{}, false
		}
		if view, ok := c.collectionOfIn(value.X, context); ok {
			// The conversion exposes exactly the prefix that fits in the array.
			// The slice's remaining capacity is inaccessible through the pointer.
			view.end, view.shortestEnd = view.start+array.Len(), nil
			if !view.dynamicStart {
				view.capEnd = view.end
			}
			view.length, view.value = nil, value
			return view, true
		}
	case *ssa.Slice:
		if view, ok := c.collectionOfIn(value.X, context); ok {
			low := int64(0)
			if value.Low != nil {
				var known bool
				low, known = integerValue(value.Low)
				if !known {
					if value.Low == value.High {
						view.end, view.shortestEnd, view.length, view.value = view.start, nil, nil, value
						if view.dynamicStart {
							view.offset = nil
						} else {
							view.offset = value.Low
						}
						view.dynamicStart = true
						if sameLength(value.Max, value.Low) {
							view.capEnd = view.end
						}
						return view, true
					}
					return collectionView{}, false
				}
			}
			if value.High != nil {
				view.shortestEnd = nil // A successful reslice has its explicit upper bound.
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
		return c.collectionOfIn(value.X, context)
	case *ssa.Field:
		if origin := c.projectedReference(value.X, []int{value.Field}, make(map[valueVisit]bool), nil); origin != nil {
			return c.collectionOfIn(origin, context)
		}
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if array, ok := value.Type().Underlying().(*types.Array); ok {
				if storage, ok := addressOf(value.X); ok {
					return collectionView{storage: storage, element: array.Elem(), end: array.Len(), capEnd: array.Len(), value: value}, true
				}
				if view, ok := c.collectionOfIn(value.X, context); ok {
					view.value = value
					return view, true
				}
			}
			if origin := localLoadOrigin(value); origin != nil {
				return c.collectionOfIn(origin, context)
			}
			if location, ok := c.collectionAddress(value.X); ok {
				if origin := c.storedReference(location, instructionPoint(value)); origin != nil {
					return c.collectionOfIn(origin, context)
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

// Collection views need one backing layout. A conversion can be legal for
// multiple array lengths even though a single view cannot describe them.
func collectionOperandShape(t types.Type) types.Type {
	parameter, ok := types.Unalias(t).(*types.TypeParam)
	if !ok {
		return t.Underlying()
	}
	terms, err := typeparams.NormalTerms(parameter)
	if err != nil || len(terms) == 0 {
		return nil
	}
	shape := terms[0].Type().Underlying()
	for _, term := range terms[1:] {
		if !types.Identical(shape, term.Type().Underlying()) {
			return nil
		}
	}
	return shape
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
	checkedHandoff := false
	for index := point.index - 1; index >= 0; index-- {
		if !c.work.take() {
			return nil
		}
		instruction := point.block.Instrs[index]
		if allocation, ok := instruction.(ssa.Value); ok && allocation == view.storage.root {
			if call, ok := allocation.(*ssa.Call); ok {
				if prefix, appended, known := c.appendViews(&call.Call, context); known {
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
			if problem != nil && view.guaranteedEnd() <= firstMissing(covered, view.start) {
				if source, _, _, known := c.rootSource(allocation); known && source.Kind == sourceChoice {
					problem = problem.uncertain()
				}
			}
			return problem.step("source", allocation.Pos(), "collection contains zero-initialized elements")
		}
		if store, ok := instruction.(*ssa.Store); ok {
			destination, exact := c.collectionAddress(store.Addr)
			if exact && destination.root == view.storage.root {
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
					if offset < 0 && rangeCovered(covered, view.start, view.end) {
						return nil
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
					} else if problem := c.projectedStorageProblem(view.element, remaining, context.use, func(field types.Type, useMode valueUse) *zeroProblem {
						fieldContext := context
						fieldContext.use = useMode
						return c.problem(store.Val, nil, field, use, fieldContext, values)
					}); problem != nil {
						problem.path = fieldPath(elementLabel(offset), problem.path)
						return problem.uncertain().step("assignment", store.Pos(), "assign field of a possibly overlapping element")
					}
					if offset < 0 {
						before := flowPoint{point.block, index}
						return c.collectionAtProblem(view, before, use, context, values, covered, seen).
							afterUnknownMutation(store.Pos(), "indexed write", "collection elements")
					}
					covered = coverRange(covered, offset, offset+1)
				}
			} else if _, array := store.Val.Type().Underlying().(*types.Array); !exact && array {
				// A whole array store through a converted slice pointer
				// replaces a range, even when it has no single storage path.
				if target, ok := c.collectionOf(store.Addr); ok && sameStorage(target.storage, view.storage) {
					target = boundedView(target, context)
					written := intersectView(view, target.start, target.possibleEnd())
					if !rangeCovered(covered, written.start, written.end) {
						var problem *zeroProblem
						switch source := store.Val.(type) {
						case *ssa.Const:
							if zeroConstant(source) {
								problem = c.unfilledCollection(written, covered, reasonZeroValue, context.use)
							}
						case *ssa.UnOp:
							if contents, known := c.collectionOf(source); known && !target.dynamicStart {
								problem = c.copiedCollectionProblem(contents, written, target.start, instructionPoint(source), use, context, values, covered, seen)
							} else {
								problem = c.problem(source, nil, source.Type(), use, context, values)
							}
						default:
							problem = c.problem(source, nil, source.Type(), use, context, values)
						}
						if problem != nil {
							if target.dynamicStart {
								problem = problem.uncertain()
							}
							return problem.step("assignment", store.Pos(), "assign through a slice-to-array pointer")
						}
						if !target.dynamicStart {
							covered = coverRange(covered, written.start, written.end)
						}
					}
				}
			}
		}
		if rangeCovered(covered, view.start, view.end) {
			return nil
		}
		before := flowPoint{point.block, index}
		if !checkedHandoff && storageHandoff(instruction) {
			// Earlier handoffs have fewer opportunities to share the storage
			// and no additional uncovered elements.
			checkedHandoff = true
			if problem := c.collectionHandoffProblem(view, covered, flowPoint{point.block, index + 1}, context.use); problem != nil {
				return problem.step("mutation", instructionPosition(instruction, before), "shared collection storage may change during synchronization")
			}
		}
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
					if destination, ok := c.collectionOfIn(common.Args[0], context); ok && sameStorage(destination.storage, view.storage) {
						destination = boundedView(destination, context)
						if destination.size() == 0 || !rangesOverlap(destination.start, destination.possibleEnd(), view.start, view.end) {
							continue
						}
						written := intersectView(view, destination.start, destination.possibleEnd())
						if problem := c.unfilledCollection(written, covered, reasonCleared, context.use); problem != nil {
							if written.end < 0 || destination.guaranteedEnd() < written.end || destination.dynamicStart || execution.conditional {
								problem = problem.uncertain()
							}
							return problem.step("source", call.Pos(), "clear sets collection elements to zero")
						}
					}
				case "copy":
					// A destination selected by a branch can overlap this view
					// even when collectionOf cannot resolve it. An extension can
					// reuse the source's branch handling and preserve prior values
					// on edges that leave the slots untouched.
					if destination, ok := c.collectionOfIn(common.Args[0], context); ok && sameStorage(destination.storage, view.storage) {
						destination = boundedView(destination, context)
						if source, ok := c.collectionOfIn(common.Args[1], context); ok {
							source = boundedView(source, context)
							if destination.dynamicStart || source.dynamicStart {
								continue
							}
							transferred := intersectView(view, destination.start, endFromCount(destination.start, minEnd(destination.size(), source.size())))
							covers := !execution.conditional && copyCoversView(destination, source, view)
							guaranteedEnd := destination.start
							if !execution.conditional {
								guaranteedEnd = copyGuaranteedEnd(destination, source)
							}
							definite := covers || transferred.end >= 0 && guaranteedEnd >= transferred.end
							if problem := c.copiedCollectionProblem(source, transferred, destination.start, before, use, context, values, covered, seen); problem != nil {
								if !definite {
									problem = problem.uncertain()
								}
								return problem
							}
							if covers {
								return nil
							}
							if end := minEnd(guaranteedEnd, transferred.end); transferred.start < end {
								covered = coverRange(covered, transferred.start, end)
							}
							if !definite && transferred.size() != 0 && (source.shortestEnd != nil || destination.shortestEnd != nil) {
								// A longer alternative can overwrite earlier contents
								// even when it does not establish coverage on all paths.
								return c.collectionAtProblem(view, before, use, context, values, covered, seen).uncertain()
							}
						} else {
							problem, end := c.branchCopyProblem(common.Args[1], destination, view, before, use, context, values, covered, seen)
							if problem != nil {
								return problem
							}
							if !execution.conditional && end > destination.start {
								covered = coverRange(covered, destination.start, end)
							}
						}
					}
				case "append":
					if destination, source, known := c.appendWriteViews(common, context); known &&
						sameStorage(destination.storage, view.storage) && destination.size() != 0 {
						transferred := intersectView(view, destination.start, destination.end)
						if problem := c.copiedCollectionProblem(source, transferred, destination.start, before, use, context, values, covered, seen); problem != nil {
							if destination.guaranteedEnd() < transferred.end {
								problem = problem.uncertain()
							}
							return problem
						}
						end := destination.guaranteedEnd()
						if result, ok := view.value.(*ssa.Call); ok && &result.Call == common {
							// Each slot added to this result comes from the
							// source, even when the result can be shorter.
							end = destination.end
						}
						if end = min(end, transferred.end); transferred.start < end {
							covered = coverRange(covered, transferred.start, end)
						}
						if transferred.start < transferred.end && end < transferred.end {
							return c.collectionAtProblem(view, before, use, context, values, covered, seen).uncertain()
						}
					}
				}
			} else {
				for _, remaining := range uncoveredViews(view, covered) {
					storage := mutationTarget{remaining.storage, &elementRange{remaining.start, remaining.end}}
					if reason, problem := c.decoderInvalidation(call, storage, view.element, before, context); reason != "" {
						if problem == nil {
							problem = c.failedResultProblem(view.element, reason, context.use, make(map[types.Type]valueUse))
							if problem != nil {
								problem.path = fieldPath(elementLabel(remaining.start), problem.path)
							}
						}
						if problem != nil {
							detail := " may leave partially decoded collection elements without checked success"
							if reason == reasonInputMayBeZero {
								detail = " decodes into collection storage without establishing its domain contract"
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
		if filled, handoff := c.filledByLoop(view, predecessor, point.block, use, context.use, values); filled || handoff != nil {
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

func (c *zeroChecker) collectionHandoffProblem(view collectionView, covered []elementRange, after flowPoint, use valueUse) *zeroProblem {
	if _, arrayValue := view.storage.root.Type().Underlying().(*types.Array); arrayValue || !sharedStorage(view.storage.root) {
		return nil
	}
	problem := c.failedResultProblem(view.element, reasonInputMayBeZero, use, make(map[types.Type]valueUse))
	if problem == nil {
		return nil
	}
	for _, remaining := range uncoveredViews(view, covered) {
		storage := mutationTarget{remaining.storage, &elementRange{remaining.start, remaining.end}}
		if c.storageSharedBefore(storage, after) && !c.work.exhausted() {
			problem.path = fieldPath(elementLabel(remaining.start), problem.path)
			return problem
		}
	}
	return nil
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
	// With the same slice header, a sufficiently long source writes every
	// slot that exists, even when the destination length varies.
	if destination.value == view.value && destination.start <= view.start && view.end >= 0 &&
		view.end <= destination.end && source.guaranteedSize() >= destination.size() {
		return true
	}
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

func copyGuaranteedEnd(destination, source collectionView) int64 {
	return destination.start + max(0, min(destination.guaranteedSize(), source.guaranteedSize()))
}

// A direct branch merge can select different backing storage and lengths. Only
// the prefix written by every feasible source replaces the earlier contents.
func (c *zeroChecker) branchCopyProblem(value ssa.Value, destination, view collectionView, point, use flowPoint, context flowContext,
	values map[valueVisit]bool, covered []elementRange, seen map[collectionVisit]bool) (*zeroProblem, int64) {
	if destination.dynamicStart || destination.end < 0 {
		return nil, destination.start
	}
	end, feasible := destination.end, false
	for source, edge := range c.branchCopySources(value, point, context) {
		feasible = true
		if source.dynamicStart || source.end < 0 {
			end = destination.start
			continue
		}
		writeEnd := destination.start + min(destination.size(), source.size())
		transferred := intersectView(view, destination.start, writeEnd)
		if problem := c.copiedCollectionProblem(source, transferred, destination.start, point, use, edge, values, covered, seen); problem != nil {
			return problem.uncertain(), destination.start
		}
		end = min(end, copyGuaranteedEnd(destination, source))
	}
	if !feasible || c.work.exhausted() {
		return nil, destination.start
	}
	return nil, end
}

func (c *zeroChecker) branchCopySources(value ssa.Value, point flowPoint, context flowContext) iter.Seq2[collectionView, flowContext] {
	return func(yield func(collectionView, flowContext) bool) {
		phi, ok := value.(*ssa.Phi)
		if !ok || phi.Block() != point.block || len(phi.Edges) != 2 || slices.ContainsFunc(phi.Block().Preds, phi.Block().Dominates) {
			return
		}
		for index, incoming := range phi.Edges {
			if !c.work.take() {
				return
			}
			edge := c.edgeContext(context, phi.Block(), index)
			if !edge.possible {
				continue
			}
			source, known := c.collectionOfIn(incoming, edge)
			if known {
				source = boundedView(source, edge)
			} else {
				source.end = -1
			}
			if !yield(source, edge) {
				return
			}
		}
	}
}

// Read a copied field in its source slot at the copy. Observe any storage it
// references at use, when the copied or appended value is consumed.
func (c *zeroChecker) collectionWriteProblem(execution executedCall, location address, t types.Type, use flowPoint,
	values map[valueVisit]bool, seen map[memoryVisit]bool) (problem *zeroProblem, definite, overlaps bool) {
	common := execution.call.Common()
	builtin, ok := common.Value.(*ssa.Builtin)
	if !ok {
		return nil, false, false
	}
	var destination, source collectionView
	context := execution.context
	switch builtin.Name() {
	case "clear", "copy":
		destination, ok = c.collectionOfIn(common.Args[0], context)
	case "append":
		destination, source, ok = c.appendWriteViews(common, context)
	default:
		return nil, false, false
	}
	base := len(destination.storage.path)
	if !ok || destination.storage.root != location.root || len(location.path) <= base || !pathPrefix(location.path, destination.storage.path) {
		return nil, false, false
	}
	destination = boundedView(destination, context)
	index := int64(location.path[base])
	end := minEnd(destination.possibleEnd(), destination.capEnd)
	if destination.size() == 0 || index < destination.start || end >= 0 && index >= end {
		return nil, false, false
	}
	_, asynchronous := execution.call.(*ssa.Go)
	definite = !execution.conditional && !asynchronous && !destination.dynamicStart && index < destination.guaranteedEnd()
	if builtin.Name() == "clear" {
		problem := c.zeroType(t, "", context.use)
		if problem != nil {
			problem.reason = reasonCleared
		}
		return problem.step("source", execution.call.Pos(), "clear sets collection elements to zero"), definite, true
	}

	if builtin.Name() == "copy" {
		source, ok = c.collectionOfIn(common.Args[1], context)
		if !ok {
			problem, covered, overlaps := c.branchCopyFieldProblem(common.Args[1], destination, location, t, execution, use, values, seen)
			return problem, definite && covered, overlaps
		}
	}
	source = boundedView(source, context)
	problem, overlaps = c.copiedFieldProblem(source, destination, location, t, execution, use, values, seen)
	return problem, definite && index-destination.start < source.guaranteedSize() && overlaps && !c.work.exhausted(), overlaps
}

func (c *zeroChecker) branchCopyFieldProblem(value ssa.Value, destination collectionView, location address, t types.Type, execution executedCall,
	use flowPoint, values map[valueVisit]bool, seen map[memoryVisit]bool) (problem *zeroProblem, covered, overlaps bool) {
	if destination.dynamicStart || destination.end < 0 {
		return nil, false, false
	}
	covered = true
	for source, edge := range c.branchCopySources(value, execution.before, execution.context) {
		if source.dynamicStart || source.end < 0 {
			covered = false
			continue
		}
		branch := execution
		branch.context = edge
		problem, written := c.copiedFieldProblem(source, destination, location, t, branch, use, values, seen)
		if problem != nil {
			return problem, false, true
		}
		index := int64(location.path[len(destination.storage.path)])
		covered = covered && written && index-destination.start < source.guaranteedSize()
		overlaps = overlaps || written
	}
	return nil, covered && overlaps && !c.work.exhausted(), overlaps
}

func (c *zeroChecker) copiedFieldProblem(source, destination collectionView, location address, t types.Type, execution executedCall,
	use flowPoint, values map[valueVisit]bool, seen map[memoryVisit]bool) (*zeroProblem, bool) {
	if destination.dynamicStart || source.dynamicStart {
		return nil, false
	}
	base := len(destination.storage.path)
	index := int64(location.path[base])
	offset := source.start + index - destination.start
	end := minEnd(source.end, source.capEnd)
	if end >= 0 && offset >= end {
		return nil, false
	}
	sourcePath := append(slices.Clone(source.storage.path), int(offset))
	source.storage.path = append(sourcePath, location.path[base+1:]...)
	operation := execution.call.Common().Value.(*ssa.Builtin).Name()
	problem := c.memoryAtProblem(source.storage, t, execution.before, use, execution.context, values, seen).
		step("assignment", execution.call.Pos(), operation+" collection field")
	return problem, true
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

func (c *zeroChecker) appendViews(call *ssa.CallCommon, context flowContext) (collectionView, collectionView, bool) {
	before, added, known := c.appendOperands(call, context)
	spare := before.capEnd - before.end
	// Follow one backing allocation only when all lengths fit or all allocate.
	known = known && added.size() >= 0 && (added.size() <= spare || added.guaranteedSize() > spare)
	return before, added, known
}

func (c *zeroChecker) appendOperands(call *ssa.CallCommon, context flowContext) (collectionView, collectionView, bool) {
	builtin, ok := call.Value.(*ssa.Builtin)
	if !ok || builtin.Name() != "append" || len(call.Args) != 2 {
		return collectionView{}, collectionView{}, false
	}
	before, beforeKnown := c.collectionOfIn(call.Args[0], context)
	added, addedKnown := c.collectionOfIn(call.Args[1], context)
	before, added = boundedView(before, context), boundedView(added, context)
	known := beforeKnown && addedKnown && !before.dynamicStart && !added.dynamicStart &&
		before.end >= 0 && before.end == before.guaranteedEnd() && before.capEnd >= before.end
	return before, added, known
}

// An append that may allocate has no guaranteed write on the old backing
// storage. A possible write is limited to the elements that fit there.
func (c *zeroChecker) appendWriteViews(call *ssa.CallCommon, context flowContext) (destination, source collectionView, known bool) {
	destination, source, known = c.appendOperands(call, context)
	if !known {
		return destination, source, false
	}
	destination.start = destination.end
	spare := destination.capEnd - destination.start
	added := source.size()
	switch {
	case source.guaranteedSize() > spare:
		destination.setLengthBounds(0, 0)
	case added >= 0 && added <= spare:
		destination.setLengthBounds(max(0, source.guaranteedSize()), added)
	default:
		destination.setLengthBounds(0, spare)
	}
	destination.length = nil
	return destination, source, true
}

// Even with an unresolved low bound, an append can only write between the
// slice's high bound and its capacity in the original backing storage.
func (c *zeroChecker) unresolvedAppendWriteView(call *ssa.CallCommon) (collectionView, bool) {
	if len(call.Args) != 2 {
		return collectionView{}, false
	}
	slice, ok := call.Args[0].(*ssa.Slice)
	if !ok {
		return collectionView{}, false
	}
	base, known := c.collectionOf(slice.X)
	if !known || base.dynamicStart {
		return collectionView{}, false
	}
	start, exactStart := base.start, false
	if slice.High == nil {
		high := base.guaranteedEnd()
		start = max(start, high)
		exactStart = high >= 0 && high == base.end
	} else if high, fixed := integerValue(slice.High); fixed {
		start += high
		exactStart = true
	}
	end := base.capEnd
	if limit, fixed := integerValue(slice.Max); fixed {
		end = minEnd(end, base.start+limit)
	}
	if sameLength(slice.High, slice.Max) {
		end = start
	}
	if source, known := c.collectionOf(call.Args[1]); known {
		switch {
		case source.size() == 0 || end >= 0 && source.guaranteedSize() > end-start:
			end = start
		case exactStart && source.size() >= 0:
			end = minEnd(end, start+source.size())
		}
	}
	return collectionView{storage: base.storage, element: base.element, start: start, end: end, capEnd: end, value: slice}, true
}

// Only the transferred slots can change.
func (c *zeroChecker) builtinWriteView(call *ssa.CallCommon) (collectionView, bool) {
	name := call.Value.(*ssa.Builtin).Name()
	if name == "append" {
		if destination, _, known := c.appendWriteViews(call, flowContext{}); known {
			return destination, true
		}
	}
	destination, known := c.collectionOf(call.Args[0])
	if !known {
		return collectionView{}, false
	}
	switch name {
	case "copy":
		if source, known := c.collectionOf(call.Args[1]); known {
			destination.end = endFromCount(destination.start, minEnd(destination.size(), source.size()))
			destination.shortestEnd = nil
		}
	case "append":
		if !destination.dynamicStart {
			destination.start = max(destination.start, destination.guaranteedEnd())
		}
		destination.end = destination.capEnd
		destination.shortestEnd = nil
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
			view.end, view.shortestEnd = view.lengthBase+upper, nil
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
