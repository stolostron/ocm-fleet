package domainconstruct

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/diagnostic"
)

// Zero comparisons are a convention check, even for values trusted by contract:
// absence belongs in an optional type, and primitive input is checked before
// construction. Keep this independent of the flow analysis of invalid uses.
func (c *zeroChecker) checkZeroComparisons() {
	construction := &checker{pass: c.pass, policies: c.policies}
	for _, file := range c.pass.Files {
		var ancestors []ast.Node
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				ancestors = ancestors[:len(ancestors)-1]
				return true
			}
			report := func(value ast.Expr, pos token.Pos) {
				p := c.comparisonPolicy(value)
				if p == nil || p.fact.ZeroValid {
					return
				}
				c.comparisons[pos] = true
				if !construction.approved(p, ancestors) {
					c.pass.Report((diagnostic.Finding{
						Rule: "domainzero.zero-comparison", Certainty: diagnostic.Known,
						Operation: "comparison", Value: expressionText(c.pass.Fset, value), Type: typeIdentity(p.typ),
						Reason: "zero-check-on-required-value", Pos: pos,
						Message: "zero comparison for " + p.name + " is unnecessary; remove the check or represent absence explicitly",
					}).Diagnostic())
				}
			}
			switch node := node.(type) {
			case *ast.BinaryExpr:
				if node.Op == token.EQL || node.Op == token.NEQ {
					if constantZero(c.pass.TypesInfo.Types[node.Y].Value) {
						report(node.X, node.OpPos)
					} else if constantZero(c.pass.TypesInfo.Types[node.X].Value) {
						report(node.Y, node.OpPos)
					}
				}
			case *ast.SwitchStmt:
				if node.Tag != nil {
					for _, clause := range node.Body.List {
						for _, value := range clause.(*ast.CaseClause).List {
							if constantZero(c.pass.TypesInfo.Types[value].Value) {
								report(node.Tag, value.Pos())
							}
						}
					}
				}
			}
			ancestors = append(ancestors, node)
			return true
		})
	}
}

func (c *zeroChecker) comparisonPolicy(expression ast.Expr) *policy {
	expression = ast.Unparen(expression)
	if t := c.pass.TypesInfo.TypeOf(expression); t != nil {
		if _, pointer := t.Underlying().(*types.Pointer); !pointer {
			if p := c.policies.lookup(t); p != nil {
				return p
			}
		}
	}
	if call, ok := expression.(*ast.CallExpr); ok && len(call.Args) == 1 {
		if c.pass.TypesInfo.Types[call.Fun].IsType() {
			// Only direct scalar representation conversions preserve the meaning
			// of a zero comparison. Arbitrary function calls do not.
			target, source := c.pass.TypesInfo.TypeOf(call), c.pass.TypesInfo.TypeOf(call.Args[0])
			if _, scalar := target.Underlying().(*types.Basic); scalar && types.Identical(target.Underlying(), source.Underlying()) {
				return c.comparisonPolicy(call.Args[0])
			}
		}
		if builtin, ok := expressionObject(c.pass.TypesInfo, call.Fun).(*types.Builtin); ok && builtin.Name() == "len" {
			if basic, ok := c.pass.TypesInfo.TypeOf(call.Args[0]).Underlying().(*types.Basic); ok && basic.Info()&types.IsString != 0 {
				return c.comparisonPolicy(call.Args[0])
			}
		}
	}
	return nil
}
