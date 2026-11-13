package domainconstruct

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// Re-reading an unchanged table field produces a different SSA value. Its
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

func (c *zeroChecker) sameReadValue(first, second ssa.Value) bool {
	if first == second {
		return true
	}
	if field, ok := first.(*ssa.Field); ok {
		if other, ok := second.(*ssa.Field); ok && field.Field == other.Field {
			return c.sameReadValue(field.X, other.X)
		}
	}
	left, leftLoad := first.(*ssa.UnOp)
	right, rightLoad := second.(*ssa.UnOp)
	if !leftLoad || !rightLoad || left.Op != token.MUL || right.Op != token.MUL {
		return false
	}
	leftAddress, leftKnown := addressOf(left.X)
	rightAddress, rightKnown := addressOf(right.X)
	if !leftKnown || !rightKnown || !sameStorage(leftAddress, rightAddress) {
		return false
	}
	start, end := instructionPoint(left), instructionPoint(right)
	if start.block == end.block {
		if start.index > end.index {
			start, end = end, start
		}
	} else if end.block.Dominates(start.block) {
		start, end = end, start
	} else if !start.block.Dominates(end.block) {
		return false
	}
	return c.storageUnchanged(leftAddress, start, end, make(map[*ssa.BasicBlock]bool))
}
