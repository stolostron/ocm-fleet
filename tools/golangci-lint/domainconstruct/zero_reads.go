package domainconstruct

import (
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"
)

type storageRead struct {
	storage address
	point   flowPoint
}

// Re-reading an unchanged field produces a different SSA value. Its
// zero fact still applies when no intervening effect can change the field.
func (c *zeroChecker) unchangedReadFact(value ssa.Value, context flowContext) truth {
	if _, scalar := value.Type().Underlying().(*types.Basic); !scalar {
		return unknown
	}
	switch value.(type) {
	case *ssa.UnOp, *ssa.Field:
		for other, state := range context.facts {
			if c.sameReadValue(value, other) {
				return state
			}
		}
	}
	return unknown
}

// Aggregate inspection observes storage rather than a scalar SSA value. Reuse
// a checked read of the exact field at that observation point; this establishes
// only nonzero state, not the domain type's other invariants.
func (c *zeroChecker) storageReadFact(storage address, t types.Type, point flowPoint, context flowContext) truth {
	if _, scalar := t.Underlying().(*types.Basic); !scalar {
		return unknown
	}
	for value, state := range context.facts {
		if state != unknown && types.Identical(value.Type(), t) && c.sameStorageRead(value, storage, point) {
			return state
		}
	}
	return unknown
}

func (c *zeroChecker) sameReadValue(first, second ssa.Value) bool {
	if first == second {
		return true
	}
	if field, ok := first.(*ssa.Field); ok {
		if other, ok := second.(*ssa.Field); ok && field.Field == other.Field {
			return c.sameReadValue(field.X, other.X)
		}
	}
	// Most branch facts are comparisons, not storage reads. Reject them before
	// constructing the queried value's address for each unrelated fact.
	storage, point, known := c.loadedStorage(second)
	return known && c.sameStorageRead(first, storage, point)
}

func (c *zeroChecker) sameStorageRead(value ssa.Value, storage address, point flowPoint) bool {
	other, start, known := c.loadedStorage(value)
	if !known || !sameStorage(storage, other) {
		return false
	}
	end := point
	if start == end {
		return true
	}
	if start.block == end.block {
		if start.index > end.index {
			start, end = end, start
		}
	} else if end.block.Dominates(start.block) {
		start, end = end, start
	} else if !start.block.Dominates(end.block) {
		return false
	}
	return c.storageUnchanged(storage, start, end, make(map[*ssa.BasicBlock]bool))
}

// A field extracted from a copied aggregate retains the copy's read point.
// Address paths here are exact; a dynamic index cannot identify one element.
// Locations depend only on SSA and can be reused across branches; truth and
// intervening-effect checks remain specific to each query.
func (c *zeroChecker) loadedStorage(value ssa.Value) (address, flowPoint, bool) {
	switch value.(type) {
	case *ssa.Field, *ssa.UnOp:
	default:
		return address{}, flowPoint{}, false
	}
	if read, found := c.reads[value]; found {
		return read.storage, read.point, read.storage.root != nil
	}
	var read storageRead
	switch value := value.(type) {
	case *ssa.Field:
		storage, point, known := c.loadedStorage(value.X)
		if known {
			storage.path = append(slices.Clone(storage.path), value.Field)
			read = storageRead{storage, point}
		}
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if storage, known := addressOf(value.X); known {
				read = storageRead{storage, instructionPoint(value)}
			}
		}
	}
	if c.reads == nil {
		c.reads = make(map[ssa.Value]storageRead)
	}
	c.reads[value] = read
	return read.storage, read.point, read.storage.root != nil
}

// Every path between observations must leave the storage untouched.
// Unresolved aliases and deferred effects cannot establish that proof.
func (c *zeroChecker) storageUnchanged(storage address, start, end flowPoint, seen map[*ssa.BasicBlock]bool) bool {
	if !start.block.Dominates(end.block) {
		return false
	}
	if unchanged, found := seen[end.block]; found {
		return unchanged
	}
	seen[end.block] = false
	low := 0
	if start.block == end.block {
		low = start.index + 1
		if low > end.index {
			return false
		}
	}
	for index := low; index < end.index; index++ {
		instruction := end.block.Instrs[index]
		if _, cleanup := instruction.(*ssa.RunDefers); cleanup {
			return false
		}
		if _, scheduled := instruction.(*ssa.Defer); scheduled {
			continue
		}
		if sharedStorage(storage.root) {
			if channelHandoff(instruction) || storageHandoff(instruction) &&
				c.storageSharedBefore(mutationTarget{address: storage}, flowPoint{end.block, index + 1}) {
				return false
			}
		}
		if store, ok := instruction.(*ssa.Store); ok {
			target, known := c.collectionAddress(store.Addr)
			if !known {
				return false
			}
			if target.root == storage.root {
				if storagePathsOverlap(target.path, storage.path) {
					return false
				}
			} else {
				// Separate allocations cannot alias. Other roots, including
				// parameters and borrowed pointers, may refer to the same storage.
				_, sourceLocal := storage.root.(*ssa.Alloc)
				_, targetLocal := target.root.(*ssa.Alloc)
				if !sourceLocal || !targetLocal {
					return false
				}
			}
		}
		if call, ok := instruction.(ssa.CallInstruction); ok {
			point, common := flowPoint{end.block, index}, call.Common()
			if builtin, ok := common.Value.(*ssa.Builtin); ok {
				switch builtin.Name() {
				case "clear", "copy", "append":
					view, known := c.builtinWriteView(common)
					if !known {
						return false
					}
					if c.collectionMayReference(view, mutationTarget{address: storage}, point, make(map[ssa.Value]bool), make(map[memoryVisit]bool)) {
						return false
					}
				}
			}
			if c.callMayMutate(common, storage, point) {
				return false
			}
		}
	}
	if start.block == end.block {
		seen[end.block] = true
		return true
	}
	for _, predecessor := range end.block.Preds {
		if !testingExitBlock(predecessor) && !c.storageUnchanged(storage, start, flowPoint{predecessor, len(predecessor.Instrs)}, seen) {
			return false
		}
	}
	seen[end.block] = true
	return true
}

func sharedStorage(value ssa.Value) bool {
	if allocation, ok := value.(*ssa.Alloc); ok {
		return allocation.Heap
	}
	return true
}

func channelHandoff(instruction ssa.Instruction) bool {
	switch instruction := instruction.(type) {
	case *ssa.Send, *ssa.Select:
		return true
	case *ssa.UnOp:
		return instruction.Op == token.ARROW
	}
	return false
}

func storageHandoff(instruction ssa.Instruction) bool {
	if channelHandoff(instruction) {
		return true
	}
	call, ok := instruction.(*ssa.Call)
	if !ok {
		return false
	}
	receiver, name := synchronizationMethod(call.Common())
	switch receiver {
	case "WaitGroup", "Cond":
		return name == "Wait"
	case "Mutex":
		return name == "Lock"
	case "RWMutex":
		return name == "Lock" || name == "RLock"
	}
	return false
}

func synchronizationMethod(call *ssa.CallCommon) (receiverName, methodName string) {
	method := callObject(call)
	if method == nil || method.Pkg() == nil || method.Pkg().Path() != "sync" {
		return "", ""
	}
	receiver := method.Type().(*types.Signature).Recv()
	if receiver == nil {
		return "", ""
	}
	named := domainType(receiver.Type())
	if named == nil {
		return "", ""
	}
	return named.Obj().Name(), method.Name()
}
