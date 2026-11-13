package domainconstruct

import (
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"
)

type storageWrite struct {
	target address
	value  ssa.Value
}

// Index the same destinations used by backward storage tracing. Calls are
// interpreted separately through their mutation contracts.
func (c *zeroChecker) indexStores(functions []*ssa.Function) {
	c.stores = make(map[ssa.Value][]storageWrite)
	for _, function := range functions {
		for _, block := range function.Blocks {
			for _, instruction := range block.Instrs {
				if value, ok := instruction.(ssa.Value); ok && localStorageRoot(value) {
					if _, indexed := c.stores[value]; !indexed {
						c.stores[value] = nil
					}
				}
				store, ok := instruction.(*ssa.Store)
				if !ok {
					continue
				}
				target, known := c.collectionAddress(store.Addr)
				if !known || !localStorageRoot(target.root) || target.root.Parent() != function {
					continue
				}
				c.stores[target.root] = append(c.stores[target.root], storageWrite{target, store.Val})
			}
		}
	}
}

func localStorageRoot(value ssa.Value) bool {
	switch value.(type) {
	case *ssa.Alloc, *ssa.Call, *ssa.Extract:
		_, pointer := value.Type().Underlying().(*types.Pointer)
		return pointer
	}
	return false
}

// Skip tracing when zero is valid and local writes cannot introduce a payload
// with a stricter zero contract. Opaque factories retain their declared contract,
// as in backward tracing. Unknown mutations only widen existing problems; they
// do not reveal new concrete interface payloads.
// Consider every store, including later writes, so cached answers need no flow
// point or branch assumptions.
func (c *zeroChecker) validZeroStorage(location address, t types.Type) bool {
	visit := valueVisit{location.root, pathKey(location.path), t}
	if valid, found := c.zeroStorage[visit]; found {
		return valid
	}
	valid := c.zeroType(t, "") == nil && c.onlyZeroStores(location, t, make(map[valueVisit]bool))
	if c.zeroStorage == nil {
		c.zeroStorage = make(map[valueVisit]bool)
	}
	c.zeroStorage[visit] = valid
	return valid
}

func (c *zeroChecker) onlyZeroStores(location address, t types.Type, seen map[valueVisit]bool) bool {
	stores, indexed := c.stores[location.root]
	if !indexed {
		return false
	}
	if _, _, _, known := c.rootSource(location.root); known {
		return false // A generic factory's initial contents need its source summary.
	}
	visit := valueVisit{location.root, pathKey(location.path), t}
	if seen[visit] {
		return false
	}
	seen[visit] = true
	defer delete(seen, visit)
	for _, store := range stores {
		if pathPrefix(location.path, store.target.path) {
			path := location.path[len(store.target.path):]
			if !c.zeroStoredValue(store.value, path, t, seen) {
				return false
			}
		} else if storagePathsOverlap(location.path, store.target.path) {
			return false
		}
	}
	return true
}

func (c *zeroChecker) zeroStoredValue(value ssa.Value, path []int, t types.Type, seen map[valueVisit]bool) bool {
	switch value := value.(type) {
	case *ssa.Const:
		return zeroConstant(value)
	case *ssa.Field:
		return c.zeroStoredValue(value.X, append([]int{value.Field}, path...), t, seen)
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if location, known := c.collectionAddress(value.X); known {
				location.path = append(slices.Clone(location.path), path...)
				return c.onlyZeroStores(location, t, seen)
			}
		}
	}
	return false
}

func storagePathsOverlap(first, second []int) bool {
	for index := range min(len(first), len(second)) {
		// A dynamic array or slice index may refer to any element at this level.
		if first[index] >= 0 && second[index] >= 0 && first[index] != second[index] {
			return false
		}
	}
	return true
}
