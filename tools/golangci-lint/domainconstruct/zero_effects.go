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
	if c.work.exhausted() {
		return mutationUnknown
	}
	if c.mutations == nil {
		c.mutations = make(map[mutationQuery]mutationKind)
	}
	c.mutations[query] = kind
	return kind
}

func (c *zeroChecker) findMutation(call *ssa.CallCommon, storage mutationTarget, point flowPoint) mutationKind {
	if _, global := storage.root.(*ssa.Global); global {
		return mutationUnknown
	}
	seen := make(map[ssa.Value]bool)
	if c.mayReferenceStorage(call.Value, storage, point, seen) {
		return mutationUnknown
	}
	kind := mutationReadOnly
	lastWrite := 0
	fact := c.callContract(call)
	offset := callArgumentIndex(call, 0)
	for index, argument := range call.Args {
		if c.mayReferenceStorage(argument, storage, point, seen) {
			parameter := fact.effectForParameter(index - offset)
			effect := parameter.Kind
			if parameter.LocalStorageOnly && borrowedRoot(storage.root) {
				effect = mutationUnknown
			}
			if effect == mutationReadOnly {
				continue
			}
			if target, exact := c.collectionAddress(argument); !exact || storage.elements != nil || !sameStorage(target, storage.address) {
				effect = mutationUnknown
			}
			if effect != mutationUnknown && index-offset == -1 && !exactMethodReceiver(call, argument) {
				effect = mutationUnknown
			}
			if effect == mutationUnknown {
				return mutationUnknown
			}
			// All remaining writes reach this exact slot. Their order matters
			// when the caller passes the same address to several parameters.
			if parameter.LastWrite > lastWrite {
				kind, lastWrite = effect, parameter.LastWrite
			}
		}
	}
	return kind
}

func exactMethodReceiver(call *ssa.CallCommon, argument ssa.Value) bool {
	if call.Signature().Recv() != nil {
		return true
	}
	// A method expression can pass an outer value to a thunk that then
	// projects the embedded receiver.
	if method := callObject(call); method != nil {
		if receiver := method.Type().(*types.Signature).Recv(); receiver != nil {
			return types.Identical(receiver.Type(), argument.Type())
		}
	}
	return false
}

func (c *zeroChecker) mayReferenceStorage(value ssa.Value, storage mutationTarget, point flowPoint, seen map[ssa.Value]bool) bool {
	if !c.work.take() {
		return true
	}
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
	case *ssa.MakeMap:
		for _, reference := range *value.Referrers() {
			if !c.work.take() {
				return true
			}
			update, ok := reference.(*ssa.MapUpdate)
			if ok && update.Map == value && update.Block() == point.block && instructionPoint(update).index < point.index {
				if c.mayReferenceStorage(update.Value, storage, point, seen) || c.mayReferenceStorage(update.Key, storage, point, seen) {
					return true
				}
			}
		}
	case *ssa.Slice:
		if view, ok := c.collectionOf(value); ok {
			view.end = view.capEnd
			return c.collectionMayReference(view, storage, point, seen)
		}
		return c.mayReferenceStorage(value.X, storage, point, seen)
	case *ssa.Field:
		if origin := c.projectedReference(value.X, []int{value.Field}, make(map[valueVisit]bool), nil); origin != nil {
			return c.mayReferenceStorage(origin, storage, point, seen)
		}
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if location, ok := addressOf(value.X); ok {
				return c.memoryMayReference(location, value.Type(), storage, instructionPoint(value), point, seen)
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
		if _, slice := value.Type().Underlying().(*types.Slice); slice {
			view.end = view.capEnd
		}
		if c.collectionMayReference(view, storage, point, seen) {
			return true
		}
	}
	if pointer, ok := value.Type().Underlying().(*types.Pointer); ok {
		if location, ok := c.collectionAddress(value); ok {
			return c.memoryMayReference(location, pointer.Elem(), storage, point, point, seen)
		}
	}
	return false
}

func (c *zeroChecker) memoryMayReference(location address, t types.Type, storage mutationTarget, point, use flowPoint, seen map[ssa.Value]bool) bool {
	if !c.work.take() {
		return true
	}
	if array, ok := t.Underlying().(*types.Array); ok {
		view := collectionView{storage: location, element: array.Elem(), end: array.Len()}
		return c.collectionMayReferenceAt(view, storage, point, use, seen, make(map[collectionVisit]bool))
	}
	if structure, ok := t.Underlying().(*types.Struct); ok {
		for index := range structure.NumFields() {
			child := address{location.root, append(slices.Clone(location.path), index)}
			if c.memoryMayReference(child, structure.Field(index).Type(), storage, point, use, seen) {
				return true
			}
		}
		return false
	}
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Interface, *types.Signature, *types.Slice:
		if origin, _ := c.referenceAt(location, point); origin != nil {
			return c.mayReferenceStorage(origin, storage, use, seen)
		}
		if borrowedRoot(location.root) {
			// An intervening borrowed write can make the origin uncertain
			// without ruling out the most recent write to this exact slot.
			for index := point.index - 1; index >= 0; index-- {
				if !c.work.take() {
					return true
				}
				if store, ok := point.block.Instrs[index].(*ssa.Store); ok {
					if target, known := c.collectionAddress(store.Addr); known && sameStorage(target, location) {
						return c.mayReferenceStorage(store.Val, storage, use, seen)
					}
				}
			}
		}
	}
	return false
}

// Walk stored elements rather than enumerating an array's declared length.
// Variadic calls and append commonly package output pointers as []any.
func (c *zeroChecker) collectionMayReference(view collectionView, storage mutationTarget, point flowPoint, values map[ssa.Value]bool) bool {
	return c.collectionMayReferenceAt(view, storage, point, point, values, make(map[collectionVisit]bool))
}

// Walk backwards to find the stored values, then follow their references at
// the original use. An older slice header can still reach newer contents.
func (c *zeroChecker) collectionMayReferenceAt(view collectionView, storage mutationTarget, point, use flowPoint, values map[ssa.Value]bool, seen map[collectionVisit]bool) bool {
	if view.size() == 0 {
		return false
	}
	if !c.work.take() {
		return true
	}
	view.end = view.possibleEnd()
	if view.storage.root != storage.root && borrowedRoot(view.storage.root) && borrowedRoot(storage.root) {
		return true
	}
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
	visit := collectionVisit{point: point, root: view.storage.root, path: pathKey(view.storage.path), start: view.start, end: view.end}
	if seen[visit] {
		return false
	}
	seen[visit] = true
	// A failed reachability search stays failed for this query. Removing it on
	// return would revisit every path through a sequence of control-flow joins.
	written := make(map[string]bool)
	for index := point.index - 1; index >= 0; index-- {
		instruction := point.block.Instrs[index]
		if call, ok := instruction.(*ssa.Call); ok {
			common := call.Common()
			if builtin, ok := common.Value.(*ssa.Builtin); ok && builtin.Name() == "copy" {
				if !c.work.take() {
					return true
				}
				if source, known := c.copiedReferenceSource(common, view); known &&
					c.collectionMayReferenceAt(source, storage, flowPoint{point.block, index}, use, values, seen) {
					return true
				}
			}
		}
		store, ok := instruction.(*ssa.Store)
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
		if !slices.Contains(destination.path, -1) {
			key := pathKey(destination.path)
			if written[key] {
				continue
			}
			written[key] = true
		}
		if c.mayReferenceStorage(store.Val, storage, use, values) {
			return true
		}
	}
	for _, predecessor := range point.block.Preds {
		if c.collectionMayReferenceAt(view, storage, flowPoint{predecessor, len(predecessor.Instrs)}, use, values, seen) {
			return true
		}
	}
	return false
}

func (c *zeroChecker) copiedReferenceSource(call *ssa.CallCommon, observed collectionView) (collectionView, bool) {
	destination, known := c.builtinWriteView(call)
	if !known || destination.dynamicStart || destination.end < 0 || !sameStorage(destination.storage, observed.storage) {
		return collectionView{}, false
	}
	overlap := intersectView(observed, destination.start, destination.end)
	if overlap.size() == 0 {
		return collectionView{}, false
	}
	source, known := c.collectionOf(call.Args[1])
	if !known || source.dynamicStart || source.end < 0 {
		return collectionView{}, false
	}
	source.start += overlap.start - destination.start
	source.end = source.start + overlap.size()
	return source, true
}

func (storage mutationTarget) overlaps(target address) bool {
	if target.root != storage.root {
		return borrowedRoot(target.root) && borrowedRoot(storage.root)
	}
	if !storagePathsOverlap(target.path, storage.path) {
		return false
	}
	if storage.elements != nil && len(target.path) > len(storage.path) {
		index := int64(target.path[len(storage.path)])
		return index < 0 || index >= storage.elements.start && (storage.elements.end < 0 || index < storage.elements.end)
	}
	return true
}

func borrowedRoot(value ssa.Value) bool {
	switch value.(type) {
	case *ssa.Global, *ssa.Parameter, *ssa.FreeVar:
		return true
	}
	return false
}

func borrowedScalarMayAlias(location, destination address, context flowContext) bool {
	if location.root == destination.root || len(location.path)+len(destination.path) != 0 {
		return false
	}
	left, first := location.root.(*ssa.Parameter)
	right, second := destination.root.(*ssa.Parameter)
	if !first || !second {
		return false
	}
	pointer, ok := left.Type().Underlying().(*types.Pointer)
	other, alsoPointer := right.Type().Underlying().(*types.Pointer)
	if !ok || !alsoPointer {
		return false
	}
	basic, scalar := pointer.Elem().Underlying().(*types.Basic)
	if !scalar || !types.Identical(basic, other.Elem().Underlying()) {
		return false
	}
	for value, state := range context.facts {
		if comparison, ok := value.(*ssa.BinOp); ok &&
			(comparison.Op == token.NEQ && state == notZero || comparison.Op == token.EQL && state == isZero) &&
			(comparison.X == left && comparison.Y == right || comparison.Y == left && comparison.X == right) {
			return false
		}
	}
	return true
}
