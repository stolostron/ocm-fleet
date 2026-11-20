package domainconstruct

import (
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// A compiler-generated yield writes captured locals and result slots. A checked
// flag can prove a write occurred, provided every write to the domain local is
// valid and the flag cannot become true without one. This is a bounded summary
// of the local loop body, not a contract for arbitrary callbacks or iterators.
func (c *zeroChecker) iteratorInitializes(call *ssa.CallCommon, location address, t types.Type, before flowPoint, context flowContext) bool {
	for _, argument := range call.Args {
		closure, ok := argument.(*ssa.MakeClosure)
		if !ok {
			continue
		}
		function, ok := closure.Fn.(*ssa.Function)
		if !ok || function.Synthetic != "range-over-func yield" {
			continue
		}
		targetIndex := -1
		for index, binding := range closure.Bindings {
			if bound, ok := addressOf(binding); ok && sameStorage(bound, location) {
				targetIndex = index
			}
		}
		if targetIndex < 0 {
			continue
		}
		target := function.FreeVars[targetIndex]
		var writes []*ssa.Store
		valid := true
		for _, reference := range *target.Referrers() {
			switch reference := reference.(type) {
			case *ssa.Store:
				if reference.Addr != target {
					valid = false
					continue
				}
				writes = append(writes, reference)
				if (!c.returns[reference.Pos()] || context.statusMayChange) &&
					(c.problem(reference.Val, nil, t, instructionPoint(reference), c.contextAt(reference.Block()), make(map[valueVisit]bool)) != nil || c.work.exhausted()) {
					valid = false
				}
			case *ssa.UnOp, *ssa.DebugRef:
			default:
				valid = false
			}
		}
		if !valid || len(writes) == 0 {
			continue
		}
		for index, binding := range closure.Bindings {
			flag, ok := binding.(*ssa.Alloc)
			if !ok {
				continue
			}
			for value, state := range context.facts {
				read, desired := guardRead(value, state)
				if read == nil || read.X != flag || desired == nil || zeroConstant(desired) ||
					!c.zeroBefore(flag, before, make(map[flowPoint]bool)) ||
					!c.storageUnchanged(address{root: flag}, before, instructionPoint(read), make(map[*ssa.BasicBlock]bool)) {
					continue
				}
				matched, paired := false, true
				for _, reference := range *function.FreeVars[index].Referrers() {
					store, ok := reference.(*ssa.Store)
					if !ok || store.Addr != function.FreeVars[index] {
						continue
					}
					constant, ok := store.Val.(*ssa.Const)
					if !ok {
						paired = false
						break
					}
					if constant.String() != desired.String() {
						continue
					}
					matched = true
					initialized := false
					for _, write := range writes {
						if write.Block().Dominates(store.Block()) && (write.Block() != store.Block() || instructionPoint(write).index < instructionPoint(store).index) {
							initialized = true
						}
					}
					paired = paired && initialized
				}
				if matched && paired {
					return true
				}
			}
		}
	}
	return false
}

func guardRead(value ssa.Value, state truth) (*ssa.UnOp, *ssa.Const) {
	if comparison, ok := value.(*ssa.BinOp); ok && (comparison.Op == token.EQL && state == notZero || comparison.Op == token.NEQ && state == isZero) {
		for _, pair := range [][2]ssa.Value{{comparison.X, comparison.Y}, {comparison.Y, comparison.X}} {
			if read, ok := pair[0].(*ssa.UnOp); ok && read.Op == token.MUL {
				if desired, ok := pair[1].(*ssa.Const); ok {
					return read, desired
				}
			}
		}
	}
	if read, ok := value.(*ssa.UnOp); ok && read.Op == token.MUL && state == notZero {
		if basic, ok := read.Type().Underlying().(*types.Basic); ok && basic.Info()&types.IsBoolean != 0 {
			return read, ssa.NewConst(constant.MakeBool(true), types.Typ[types.Bool])
		}
	}
	return nil, nil
}

func (c *zeroChecker) zeroBefore(storage *ssa.Alloc, point flowPoint, seen map[flowPoint]bool) bool {
	if seen[point] {
		return true
	}
	seen[point] = true
	// A reaching reset or allocation must precede any possible write, even
	// when the previous execution of the iterator came from a back edge.
	for index := point.index - 1; index >= 0; index-- {
		if !c.work.take() {
			return false
		}
		instruction := point.block.Instrs[index]
		if instruction == storage {
			return true
		}
		if store, ok := instruction.(*ssa.Store); ok {
			if target, known := addressOf(store.Addr); known && target.root == storage {
				value, constant := store.Val.(*ssa.Const)
				return constant && zeroConstant(value)
			}
		}
		if call, ok := instruction.(ssa.CallInstruction); ok {
			if _, scheduled := call.(*ssa.Defer); !scheduled && c.callMayMutate(call.Common(), address{root: storage}, flowPoint{point.block, index}) {
				return false
			}
		}
	}
	for _, predecessor := range point.block.Preds {
		if !c.work.take() || !c.zeroBefore(storage, flowPoint{predecessor, len(predecessor.Instrs)}, seen) {
			return false
		}
	}
	return len(point.block.Preds) != 0
}
