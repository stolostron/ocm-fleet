package domainconstruct

import (
	"slices"

	"golang.org/x/tools/go/ssa"
)

// errors.Join is non-nil when at least one element is non-nil. Scan writes,
// rather than allocating one analysis slot per element of a potentially large
// backing array. Unknown mutations invalidate a proof about earlier contents.
func (c *zeroChecker) joinedErrorTruth(value ssa.Value, point flowPoint, context flowContext, values map[ssa.Value]bool) truth {
	if literal, ok := value.(*ssa.Const); ok && literal.IsNil() {
		return isZero
	}
	if view, ok := c.collectionOf(value); ok {
		return c.joinedViewTruth(view, point, context, values, nil, make(map[collectionVisit]bool))
	}
	return unknown
}

func (c *zeroChecker) joinedViewTruth(view collectionView, point flowPoint, context flowContext, values map[ssa.Value]bool, covered []elementRange, seen map[collectionVisit]bool) truth {
	if view.end >= 0 && view.start >= view.end {
		return isZero
	}
	visit := collectionVisit{point, view.storage.root, pathKey(view.storage.path), view.start, view.end}
	if seen[visit] {
		return unknown
	}
	seen[visit] = true
	defer delete(seen, visit)
	state := isZero
	for index := point.index - 1; index >= 0; index-- {
		instruction := point.block.Instrs[index]
		if value, ok := instruction.(ssa.Value); ok && value == view.storage.root {
			return state
		}
		if store, ok := instruction.(*ssa.Store); ok {
			if destination, ok := c.collectionAddress(store.Addr); ok && destination.root == view.storage.root {
				if len(destination.path) != len(view.storage.path)+1 || !pathPrefix(destination.path, view.storage.path) {
					return unknown
				}
				offset := int64(destination.path[len(view.storage.path)])
				if offset < 0 {
					return unknown
				}
				if offset < view.start || view.end >= 0 && offset >= view.end || rangeCovered(covered, offset, offset+1) {
					continue
				}
				actual := c.valueTruth(store.Val, context, values)
				if actual == notZero {
					return notZero
				}
				if actual == unknown {
					state = unknown
				}
				covered = coverRange(covered, offset, offset+1)
			} else if c.mayReferenceStorage(store.Val, mutationTarget{view.storage, &elementRange{view.start, view.end}}, flowPoint{point.block, index}, make(map[ssa.Value]bool)) {
				return unknown
			}
		}
		if call, ok := instruction.(ssa.CallInstruction); ok {
			builtin, builtinCall := call.Common().Value.(*ssa.Builtin)
			if builtinCall && (builtin.Name() == "len" || builtin.Name() == "cap") || typeOnlyCall(call.Common()) {
				continue
			}
			if builtinCall && builtin.Name() == "clear" {
				if target, ok := c.collectionOf(call.Common().Args[0]); ok && sameStorage(target.storage, view.storage) {
					if target.start <= view.start && (target.end >= 0 && view.end >= 0 && target.end >= view.end || target.value == view.value || sameLength(target.length, view.length)) {
						return state
					}
					if target.end >= 0 {
						covered = coverRange(covered, target.start, target.end)
						continue
					}
				}
			}
			if builtinCall {
				if (builtin.Name() == "copy" || builtin.Name() == "append" || builtin.Name() == "clear") && c.mayReferenceStorage(call.Common().Args[0], mutationTarget{view.storage, &elementRange{view.start, view.end}}, flowPoint{point.block, index}, make(map[ssa.Value]bool)) {
					return unknown
				}
			} else if c.collectionMutation(call.Common(), view, flowPoint{point.block, index}) != mutationReadOnly {
				return unknown
			}
		}
		if rangeCovered(covered, view.start, view.end) {
			return state
		}
	}
	if len(point.block.Preds) == 0 {
		return unknown
	}
	merged, found := unknown, false
	for index, predecessor := range point.block.Preds {
		edge := c.edgeContext(context, point.block, index)
		if !edge.possible {
			continue
		}
		actual := c.joinedViewTruth(view, flowPoint{predecessor, len(predecessor.Instrs)}, edge, values, slices.Clone(covered), seen)
		if found && actual != merged {
			return unknown
		}
		merged, found = actual, true
	}
	if merged == isZero && state == unknown {
		return unknown
	}
	return merged
}
