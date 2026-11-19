package domainconstruct

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

func (c *zeroChecker) checkResults(results []ssa.Value, point flowPoint, context flowContext, pos token.Pos) {
	success := context.clone()
	for _, result := range results {
		if c.errorStatus(result.Type()) {
			if !c.assume(&success, result, isZero) {
				return
			}
			success.statusMayChange = success.statusMayChange || c.deferChangesStatus(result)
		}
	}
	function := point.block.Parent()
	for function.Synthetic == "range-over-func yield" {
		function = function.Parent()
	}
	if object, _ := function.Object().(*types.Func); c.contracts.hasPresence(object) && len(results) >= function.Signature.Results().Len() {
		if !c.assume(&success, results[1], notZero) {
			return
		}
		success.statusMayChange = success.statusMayChange || c.deferChangesStatus(results[1])
	}
	for index, result := range results {
		if !c.errorStatus(result.Type()) {
			if problem := c.problem(result, nil, result.Type(), point, success, make(map[valueVisit]bool)); problem != nil {
				c.report(zeroUse{pos: pos, argument: index}, problem, "return", result)
			}
		}
	}
}

// SSA lowers a return inside an iterator loop into stores to the enclosing
// function's result slots. Check the matching result and guards together, just
// as for an ordinary return, instead of treating its failure payload as a write
// to caller-owned domain state.
func (c *zeroChecker) checkYieldReturn(store *ssa.Store, point flowPoint, context flowContext) bool {
	if store.Parent().Synthetic != "range-over-func yield" || !c.returns[store.Pos()] {
		return false
	}
	var results []ssa.Value
	first := -1
	for index, instruction := range point.block.Instrs {
		if result, ok := instruction.(*ssa.Store); ok && result.Pos() == store.Pos() {
			if first < 0 {
				first = index
			}
			results = append(results, result.Val)
		}
	}
	if first == point.index {
		c.checkResults(results, point, context, store.Pos())
	}
	return true
}

func returnPositions(files []*ast.File) map[token.Pos]bool {
	positions := make(map[token.Pos]bool)
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			if statement, ok := node.(*ast.ReturnStmt); ok {
				positions[statement.Return] = true
			}
			return true
		})
	}
	return positions
}
