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
				origin, _ := c.referenceOrigin(location, point, search)
				return origin
			}
		}
	}
	return nil
}

// storedReference recovers a local header or pointer without interpreting the
// referenced contents at assignment time. Multiple predecessors must agree;
// unknown effects and loop-carried mutations retain no assumed origin.
func (c *zeroChecker) storedReference(location address, point flowPoint) ssa.Value {
	if slices.Contains(location.path, -1) {
		return nil
	}
	search := &referenceSearch{active: make(map[memoryVisit]bool)}
	value, _ := c.referenceOrigin(location, point, search)
	return value
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
	for index := point.index - 1; index >= 0; index-- {
		if !c.work.take() {
			return nil, false
		}
		instruction := point.block.Instrs[index]
		if value, ok := instruction.(ssa.Value); ok && value == location.root {
			value := zeroStoredReference(location)
			return value, value != nil
		}
		if store, ok := instruction.(*ssa.Store); ok {
			if target, known := c.collectionAddress(store.Addr); known && (mutationTarget{address: location}).overlaps(target) {
				if target.root != location.root {
					return nil, false
				}
				if sameStorage(target, location) {
					return store.Val, true
				}
				if pathPrefix(location.path, target.path) {
					path := location.path[len(target.path):]
					value := c.projectedReference(store.Val, path, make(map[valueVisit]bool), search)
					return value, value != nil
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
	return result, len(point.block.Preds) != 0
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
