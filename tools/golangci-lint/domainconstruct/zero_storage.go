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

// Index destinations used by backward storage tracing. Copy and append can
// also write a new concrete payload into existing backing storage.
func (c *zeroChecker) indexStores(functions []*ssa.Function) {
	c.stores = make(map[ssa.Value][]storageWrite)
	for _, function := range functions {
		var collectionWrites []*ssa.CallCommon
		for _, block := range function.Blocks {
			for _, instruction := range block.Instrs {
				if value, ok := instruction.(ssa.Value); ok && localStorageRoot(value) {
					if _, indexed := c.stores[value]; !indexed {
						c.stores[value] = nil
					}
				}
				if call, ok := instruction.(ssa.CallInstruction); ok {
					if builtin, ok := call.Common().Value.(*ssa.Builtin); ok && (builtin.Name() == "copy" || builtin.Name() == "append") {
						collectionWrites = append(collectionWrites, call.Common())
					}
				}
				store, ok := instruction.(*ssa.Store)
				if !ok {
					continue
				}
				c.resetWork()
				target, known := c.collectionAddress(store.Addr)
				if !known || !localStorageRoot(target.root) || target.root.Parent() != function {
					continue
				}
				c.stores[target.root] = append(c.stores[target.root], storageWrite{target, store.Val})
			}
		}
		for _, call := range collectionWrites {
			c.resetWork()
			if view, known := c.builtinWriteView(call); known && view.size() != 0 && localStorageRoot(view.storage.root) {
				if c.collectionWrites == nil {
					c.collectionWrites = make(map[ssa.Value][]mutationTarget)
				}
				span := &elementRange{view.start, view.possibleEnd()}
				c.collectionWrites[view.storage.root] = append(c.collectionWrites[view.storage.root], mutationTarget{view.storage, span})
			}
		}
	}
}

func localStorageRoot(value ssa.Value) bool {
	switch value.(type) {
	case *ssa.MakeSlice:
		return true
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
// Consider every store, copy, and append, including later writes, so cached
// answers need no flow point or branch assumptions.
func (c *zeroChecker) validZeroStorage(location address, t types.Type) bool {
	return c.zeroType(t, "", wholeValue) == nil && c.onlyZeroStores(location, t, make(map[valueVisit]bool))
}

func (c *zeroChecker) onlyZeroStores(location address, t types.Type, seen map[valueVisit]bool) (valid bool) {
	visit := valueVisit{location.root, pathKey(location.path), t}
	if valid, found := c.zeroStorage[visit]; found {
		return valid
	}
	stores, indexed := c.stores[location.root]
	if !indexed || !c.work.take() {
		return false
	}
	if _, _, _, known := c.rootSource(location.root); known {
		return false // A generic factory's initial contents need its source summary.
	}
	if seen[visit] {
		return false
	}
	seen[visit] = true
	defer func() {
		delete(seen, visit)
		if !c.work.exhausted() {
			if c.zeroStorage == nil {
				c.zeroStorage = make(map[valueVisit]bool)
			}
			c.zeroStorage[visit] = valid
		}
	}()
	for _, write := range c.collectionWrites[location.root] {
		if !c.work.take() || write.overlaps(location) {
			return false
		}
	}
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
