package domainconstruct

import (
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"
)

type mutationTarget struct {
	address
	elements *elementRange
}

type mutationQuery struct {
	call       *ssa.CallCommon
	point      flowPoint
	root       ssa.Value
	path       string
	elements   elementRange
	collection bool
}

// Mutation reachability is shared by scalar and collection flow. Reachable
// storage may change, but passing its address never proves initialization.
func (c *zeroChecker) callMayMutate(call *ssa.CallCommon, storage address, point flowPoint) bool {
	return c.callMutation(call, storage, point) != mutationReadOnly
}

func (c *zeroChecker) callMutation(call *ssa.CallCommon, storage address, point flowPoint) mutationKind {
	return c.mutationIn(call, mutationTarget{address: storage}, point)
}

func (c *zeroChecker) collectionMutation(call *ssa.CallCommon, view collectionView, point flowPoint) mutationKind {
	return c.mutationIn(call, mutationTarget{view.storage, &elementRange{view.start, view.end}}, point)
}

func (c *zeroChecker) mutationIn(call *ssa.CallCommon, storage mutationTarget, point flowPoint) mutationKind {
	if _, builtin := call.Value.(*ssa.Builtin); builtin || typeOnlyCall(call) {
		// Collection operations are handled by their range-aware flow rules.
		// Copying a pointer element does not mutate its pointee.
		return mutationReadOnly
	}
	// Reachability depends on immutable SSA and contracts, not on branch facts.
	// Do not cache while a recursive collection lookup has an incomplete origin.
	if len(c.locating) != 0 {
		return c.findMutation(call, storage, point)
	}
	query := mutationQuery{call: call, point: point, root: storage.root, path: pathKey(storage.path)}
	if storage.elements != nil {
		query.collection, query.elements = true, *storage.elements
	}
	if kind, found := c.mutations[query]; found {
		return kind
	}
	kind := c.findMutation(call, storage, point)
	if c.mutations == nil {
		c.mutations = make(map[mutationQuery]mutationKind)
	}
	c.mutations[query] = kind
	return kind
}

func (c *zeroChecker) findMutation(call *ssa.CallCommon, storage mutationTarget, point flowPoint) mutationKind {
	seen := make(map[ssa.Value]bool)
	if c.mayReferenceStorage(call.Value, storage, point, seen) {
		return mutationUnknown
	}
	kind := mutationReadOnly
	fact := c.callContract(call)
	offset := 0
	if function := call.StaticCallee(); function != nil && function.Signature.Recv() != nil && !call.IsInvoke() {
		offset = 1
	}
	for index, argument := range call.Args {
		if c.mayReferenceStorage(argument, storage, point, seen) {
			effect := mutationUnknown
			if fact != nil {
				for _, parameter := range fact.Effects {
					if parameter.Parameter == index-offset {
						effect = parameter.Kind
						break
					}
				}
			}
			if effect == mutationReadOnly {
				continue
			}
			if target, exact := c.collectionAddress(argument); !exact || storage.elements != nil || !sameStorage(target, storage.address) {
				effect = mutationUnknown
			}
			if effect == mutationUnknown || kind != mutationReadOnly && kind != effect {
				return mutationUnknown
			}
			kind = effect
		}
	}
	return kind
}

func (c *zeroChecker) mayReferenceStorage(value ssa.Value, storage mutationTarget, point flowPoint, seen map[ssa.Value]bool) bool {
	if target, ok := c.collectionAddress(value); ok && storage.overlaps(target) {
		return true
	}
	if seen[value] {
		return false
	}
	seen[value] = true
	defer delete(seen, value)
	switch value := value.(type) {
	case *ssa.MakeClosure:
		for _, binding := range value.Bindings {
			if c.mayReferenceStorage(binding, storage, point, seen) {
				return true
			}
		}
	case *ssa.MakeInterface:
		return c.mayReferenceStorage(value.X, storage, point, seen)
	case *ssa.ChangeInterface:
		return c.mayReferenceStorage(value.X, storage, point, seen)
	case *ssa.ChangeType:
		return c.mayReferenceStorage(value.X, storage, point, seen)
	case *ssa.Slice:
		if view, ok := c.collectionOf(value); ok {
			return c.collectionMayReference(view, storage, point, seen, make(map[memoryVisit]bool))
		}
		return c.mayReferenceStorage(value.X, storage, point, seen)
	case *ssa.Field:
		if origin := c.projectedReference(value.X, []int{value.Field}, make(map[valueVisit]bool), make(map[memoryVisit]bool)); origin != nil {
			return c.mayReferenceStorage(origin, storage, point, seen)
		}
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if location, ok := addressOf(value.X); ok {
				return c.memoryMayReference(location, value.Type(), storage, instructionPoint(value), seen)
			}
		}
	case *ssa.Phi:
		for _, incoming := range value.Edges {
			if c.mayReferenceStorage(incoming, storage, point, seen) {
				return true
			}
		}
	case *ssa.Call:
		// An opaque factory may retain references supplied to it.
		for _, argument := range value.Call.Args {
			if c.mayReferenceStorage(argument, storage, point, seen) {
				return true
			}
		}
	}
	if view, ok := c.collectionOf(value); ok {
		if c.collectionMayReference(view, storage, point, seen, make(map[memoryVisit]bool)) {
			return true
		}
	}
	if pointer, ok := value.Type().Underlying().(*types.Pointer); ok {
		if location, ok := c.collectionAddress(value); ok {
			return c.memoryMayReference(location, pointer.Elem(), storage, point, seen)
		}
	}
	return false
}

func (c *zeroChecker) memoryMayReference(location address, t types.Type, storage mutationTarget, point flowPoint, seen map[ssa.Value]bool) bool {
	if array, ok := t.Underlying().(*types.Array); ok {
		return c.collectionMayReference(collectionView{storage: location, element: array.Elem(), end: array.Len()}, storage, point, seen, make(map[memoryVisit]bool))
	}
	if structure, ok := t.Underlying().(*types.Struct); ok {
		for index := range structure.NumFields() {
			child := address{location.root, append(slices.Clone(location.path), index)}
			if c.memoryMayReference(child, structure.Field(index).Type(), storage, point, seen) {
				return true
			}
		}
		return false
	}
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Interface, *types.Signature, *types.Slice:
		if origin := c.storedReference(location, point, make(map[memoryVisit]bool)); origin != nil {
			return c.mayReferenceStorage(origin, storage, point, seen)
		}
	}
	return false
}

// Walk stored elements rather than enumerating an array's declared length.
// Variadic calls and append commonly package output pointers as []any.
func (c *zeroChecker) collectionMayReference(view collectionView, storage mutationTarget, point flowPoint, values map[ssa.Value]bool, seen map[memoryVisit]bool) bool {
	if view.storage.root == storage.root && pathPrefix(storage.path, view.storage.path) {
		if len(storage.path) == len(view.storage.path) {
			if storage.elements == nil || rangesOverlap(view.start, view.end, storage.elements.start, storage.elements.end) {
				return true
			}
		} else {
			index := int64(storage.path[len(view.storage.path)])
			if index < 0 || index >= view.start && (view.end < 0 || index < view.end) {
				return true
			}
		}
	}
	visit := memoryVisit{point, view.storage.root, pathKey(view.storage.path)}
	if seen[visit] {
		return false
	}
	seen[visit] = true
	// A failed reachability search stays failed for this query. Removing it on
	// return would revisit every path through a sequence of control-flow joins.
	written := make(map[string]bool)
	for index := point.index - 1; index >= 0; index-- {
		store, ok := point.block.Instrs[index].(*ssa.Store)
		if !ok {
			continue
		}
		destination, ok := c.collectionAddress(store.Addr)
		if !ok || destination.root != view.storage.root || !pathPrefix(destination.path, view.storage.path) {
			continue
		}
		if len(destination.path) > len(view.storage.path) {
			offset := int64(destination.path[len(view.storage.path)])
			if offset >= 0 && (offset < view.start || view.end >= 0 && offset >= view.end) {
				continue
			}
		}
		key := pathKey(destination.path)
		if written[key] {
			continue
		}
		written[key] = true
		if c.mayReferenceStorage(store.Val, storage, point, values) {
			return true
		}
	}
	for _, predecessor := range point.block.Preds {
		if c.collectionMayReference(view, storage, flowPoint{predecessor, len(predecessor.Instrs)}, values, seen) {
			return true
		}
	}
	return false
}

func (storage mutationTarget) overlaps(target address) bool {
	if target.root != storage.root || !pathPrefix(target.path, storage.path) && !pathPrefix(storage.path, target.path) {
		return false
	}
	if storage.elements != nil && len(target.path) > len(storage.path) {
		index := int64(target.path[len(storage.path)])
		return index < 0 || index >= storage.elements.start && (storage.elements.end < 0 || index < storage.elements.end)
	}
	return true
}
