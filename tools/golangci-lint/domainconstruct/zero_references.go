package domainconstruct

import (
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"
)

func (c *zeroChecker) projectedReference(value ssa.Value, path []int, seen map[valueVisit]bool, memories map[memoryVisit]bool) ssa.Value {
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
		return c.projectedReference(value.X, append([]int{value.Field}, path...), seen, memories)
	case *ssa.ChangeType:
		return c.projectedReference(value.X, path, seen, memories)
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if location, ok := c.collectionAddress(value.X); ok {
				location.path = append(slices.Clone(location.path), path...)
				return c.storedReference(location, instructionPoint(value), memories)
			}
		}
	}
	return nil
}

// storedReference recovers a local header or pointer without interpreting the
// referenced contents at assignment time. Multiple predecessors must agree;
// unknown effects and loop-carried mutations retain no assumed origin.
func (c *zeroChecker) storedReference(location address, point flowPoint, seen map[memoryVisit]bool) ssa.Value {
	value, _ := c.referenceOrigin(location, point, seen)
	return value
}

func (c *zeroChecker) referenceOrigin(location address, point flowPoint, seen map[memoryVisit]bool) (ssa.Value, bool) {
	visit := memoryVisit{point, location.root, pathKey(location.path)}
	if seen[visit] {
		return nil, true // No header write was found on this cycle.
	}
	seen[visit] = true
	defer delete(seen, visit)
	for index := point.index - 1; index >= 0; index-- {
		instruction := point.block.Instrs[index]
		if value, ok := instruction.(ssa.Value); ok && value == location.root {
			value := zeroStoredReference(location)
			return value, value != nil
		}
		if store, ok := instruction.(*ssa.Store); ok {
			if target, ok := c.collectionAddress(store.Addr); ok && target.root == location.root {
				if sameStorage(target, location) {
					return store.Val, true
				}
				if pathPrefix(location.path, target.path) {
					path := location.path[len(target.path):]
					value := c.projectedReference(store.Val, path, make(map[valueVisit]bool), seen)
					return value, value != nil
				}
			}
		}
		if call, ok := instruction.(ssa.CallInstruction); ok {
			for _, argument := range call.Common().Args {
				if target, ok := addressOf(argument); ok && target.root == location.root {
					return nil, false
				}
			}
		}
	}
	var result ssa.Value
	for _, predecessor := range point.block.Preds {
		incoming, known := c.referenceOrigin(location, flowPoint{predecessor, len(predecessor.Instrs)}, seen)
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
