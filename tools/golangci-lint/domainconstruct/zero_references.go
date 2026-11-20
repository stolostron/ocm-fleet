package domainconstruct

import (
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"
)

type referenceSearch struct {
	active    map[memoryVisit]bool
	completed map[memoryVisit]referenceResult
	cycles    int
}

type referenceResult struct {
	value ssa.Value
	known bool
}

func (c *zeroChecker) projectedReference(value ssa.Value, path []int, seen map[valueVisit]bool, search *referenceSearch) ssa.Value {
	visit := valueVisit{value, pathKey(path), value.Type()}
	if seen[visit] {
		return nil
	}
	seen[visit] = true
	defer delete(seen, visit)
	if len(path) == 0 {
		return value
	}
	switch value := value.(type) {
	case *ssa.Const:
		if zeroConstant(value) {
			return zeroReference(value.Type(), path)
		}
	case *ssa.Field:
		return c.projectedReference(value.X, append([]int{value.Field}, path...), seen, search)
	case *ssa.ChangeType:
		return c.projectedReference(value.X, path, seen, search)
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if location, ok := c.collectionAddress(value.X); ok {
				location.path = append(slices.Clone(location.path), path...)
				point := instructionPoint(value)
				if search == nil {
					return c.storedReference(location, point)
				}
				if origin, known := c.referenceOrigin(location, point, search); known {
					return origin
				}
			}
		}
	}
	return nil
}

// storedReference recovers a local header or pointer without interpreting the
// referenced contents at assignment time. Multiple predecessors must agree;
// unknown effects and loop-carried mutations retain no assumed origin.
func (c *zeroChecker) storedReference(location address, point flowPoint) ssa.Value {
	if value, known := c.referenceAt(location, point); known {
		return value
	}
	return nil
}

// A possible origin only supplies reachability. It cannot prove where a later
// successful output write lands.
func (c *zeroChecker) referenceAt(location address, point flowPoint) (ssa.Value, bool) {
	if slices.Contains(location.path, -1) {
		return nil, false
	}
	search := &referenceSearch{active: make(map[memoryVisit]bool)}
	return c.referenceOrigin(location, point, search)
}

func (c *zeroChecker) referenceOrigin(location address, point flowPoint, search *referenceSearch) (origin ssa.Value, known bool) {
	visit := memoryVisit{point: point, root: location.root, path: pathKey(location.path)}
	if result, found := search.completed[visit]; found {
		return result.value, result.known
	}
	if search.active[visit] {
		search.cycles++
		return nil, true // No header write was found on this cycle.
	}
	if !c.work.take() {
		return nil, false
	}
	search.active[visit] = true
	cycles := search.cycles
	defer func() {
		delete(search.active, visit)
		if search.completed != nil && cycles == search.cycles && !c.work.exhausted() {
			search.completed[visit] = referenceResult{origin, known}
		}
	}()
	possiblyChanged := false
	for index := point.index - 1; index >= 0; index-- {
		if !c.work.take() {
			return nil, false
		}
		instruction := point.block.Instrs[index]
		if value, ok := instruction.(ssa.Value); ok && value == location.root {
			value := zeroStoredReference(location)
			return value, value != nil && !possiblyChanged
		}
		if store, ok := instruction.(*ssa.Store); ok {
			target, known := c.collectionAddress(store.Addr)
			tracked := mutationTarget{address: location}
			if !known && storeTypeMayOverwrite(store.Val.Type(), storageType(location)) &&
				c.storeMayOverlap(store.Addr, tracked, make(map[ssa.Value]bool)) {
				return nil, false
			}
			if known && tracked.overlaps(target) {
				if target.root != location.root {
					return nil, false
				}
				if sameStorage(target, location) {
					return store.Val, !possiblyChanged
				}
				if pathPrefix(location.path, target.path) {
					path := location.path[len(target.path):]
					value := c.projectedReference(store.Val, path, make(map[valueVisit]bool), search)
					return value, value != nil && !possiblyChanged
				}
				if storagePathsOverlap(target.path, location.path) {
					return nil, false
				}
			}
		}
		if _, scheduled := instruction.(*ssa.Defer); scheduled {
			continue
		}
		if borrowedRoot(location.root) && channelHandoff(instruction) {
			return nil, false
		}
		if call, ok := instruction.(ssa.CallInstruction); ok {
			common := call.Common()
			if _, launched := instruction.(*ssa.Go); launched && c.callMayMutate(common, location, flowPoint{point.block, index}) {
				return nil, false
			}
			if builtin, ok := common.Value.(*ssa.Builtin); ok {
				switch builtin.Name() {
				case "append", "clear", "copy":
					if destination, known := c.builtinWriteView(common); known && destination.storage.root == location.root &&
						storagePathsOverlap(destination.storage.path, location.path) && destination.size() != 0 {
						if len(location.path) <= len(destination.storage.path) {
							return nil, false
						}
						offset := int64(location.path[len(destination.storage.path)])
						if offset < 0 || offset >= destination.start && (destination.possibleEnd() < 0 || offset < destination.possibleEnd()) {
							return nil, false
						}
					}
				}
				continue
			}
			if closure, ok := common.Value.(*ssa.MakeClosure); ok {
				for _, binding := range closure.Bindings {
					if target, ok := addressOf(binding); ok && target.root == location.root && storagePathsOverlap(target.path, location.path) {
						return nil, false
					}
				}
			}
			for _, argument := range common.Args {
				if target, ok := addressOf(argument); ok && target.root == location.root {
					return nil, false
				}
			}
			possiblyChanged = possiblyChanged || c.sliceArgumentMayWriteSlot(common, location)
		}
	}
	if len(point.block.Preds) > 1 && search.completed == nil {
		search.completed = make(map[memoryVisit]referenceResult)
	}
	var result ssa.Value
	for _, predecessor := range point.block.Preds {
		incoming, known := c.referenceOrigin(location, flowPoint{predecessor, len(predecessor.Instrs)}, search)
		if !known {
			return nil, false
		}
		if incoming == nil {
			continue
		}
		if result != nil && incoming != result {
			return nil, false
		}
		result = incoming
	}
	return result, len(point.block.Preds) != 0 && !possiblyChanged
}

func storeTypeMayOverwrite(stored, tracked types.Type) bool {
	switch stored.Underlying().(type) {
	case *types.Struct, *types.Array:
		return true // A whole-value store may overwrite one of its slots.
	}
	return tracked == nil || types.IdenticalIgnoreTags(stored.Underlying(), tracked.Underlying())
}

// An unresolved store may overwrite a reference slot. Only reuse its earlier
// origin when every candidate address is known to be disjoint.
func (c *zeroChecker) storeMayOverlap(value ssa.Value, tracked mutationTarget, active map[ssa.Value]bool) bool {
	if !c.work.take() || active[value] {
		return true
	}
	if target, known := c.collectionAddress(value); known {
		return tracked.overlaps(target)
	}
	active[value] = true
	defer delete(active, value)
	switch value := value.(type) {
	case *ssa.Phi:
		for _, edge := range value.Edges {
			if c.storeMayOverlap(edge, tracked, active) {
				return true
			}
		}
		return false
	case *ssa.FieldAddr:
		return c.storeMayOverlap(value.X, tracked, active)
	case *ssa.IndexAddr:
		return c.storeMayOverlap(value.X, tracked, active)
	case *ssa.ChangeType:
		return c.storeMayOverlap(value.X, tracked, active)
	}
	return true
}

func (c *zeroChecker) sliceArgumentMayWriteSlot(call *ssa.CallCommon, location address) bool {
	if len(location.path) == 0 {
		return false
	}
	for index, argument := range call.Args {
		if _, slice := argument.Type().Underlying().(*types.Slice); !slice {
			continue
		}
		view, known := c.collectionOf(argument)
		if !known || view.storage.root != location.root || len(location.path) <= len(view.storage.path) ||
			!pathPrefix(location.path, view.storage.path) {
			continue
		}
		// The helper can reslice the argument up to its capacity. Check the
		// slot directly to avoid recursion through a full mutation query.
		offset := int64(location.path[len(view.storage.path)])
		if offset >= view.start && (view.capEnd < 0 || offset < view.capEnd) &&
			c.callContract(call).effectForParameter(index-callArgumentIndex(call, 0)).Kind != mutationReadOnly {
			return true
		}
	}
	return false
}

func zeroStoredReference(location address) ssa.Value {
	switch location.root.(type) {
	case *ssa.Alloc, *ssa.MakeSlice:
	default:
		return nil
	}
	t := location.root.Type()
	if pointer, ok := t.Underlying().(*types.Pointer); ok {
		t = pointer.Elem()
	}
	return zeroReference(t, location.path)
}

func zeroReference(t types.Type, path []int) ssa.Value {
	for _, index := range path {
		switch structure := t.Underlying().(type) {
		case *types.Struct:
			if index < 0 || index >= structure.NumFields() {
				return nil
			}
			t = structure.Field(index).Type()
		case *types.Array:
			t = structure.Elem()
		case *types.Slice:
			t = structure.Elem()
		default:
			return nil
		}
	}
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Interface, *types.Signature:
		return ssa.NewConst(nil, t)
	}
	return nil
}
