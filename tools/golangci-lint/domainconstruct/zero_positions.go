package domainconstruct

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ssa"
)

type zeroUse struct {
	pos  token.Pos // Original instruction position, retained for deduplication.
	call *ssa.CallCommon
}

// A use site describes the value as it appears in source. Its path excludes the
// compiler's packing slice when a source expression is one variadic argument.
type zeroUseSite struct {
	pos       token.Pos
	operation string
	path      string
}

type sourceExpression struct {
	node ast.Expr
	text string
}

// Resolve source positions and labels only for findings that survive
// deduplication. The expression cache also serves diagnostic text, without
// indexing other calls.
func (c *zeroChecker) useSite(use zeroUse, value ssa.Value, operation, path string) zeroUseSite {
	site := zeroUseSite{pos: use.pos, operation: operation, path: path}
	if use.call == nil {
		return site
	}
	if builtin, ok := use.call.Value.(*ssa.Builtin); ok && operation == "store-element" {
		site.operation = builtin.Name() // Distinguish append arguments from copy sources.
	}
	call, ok := c.sourceExpression(use.call.Pos()).node.(*ast.CallExpr)
	if !ok {
		return site
	}
	selector, _ := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if value == use.call.Value {
		if use.call.IsInvoke() && selector != nil {
			site.pos = selector.X.Pos()
		} else {
			site.pos = call.Fun.Pos()
		}
		return site
	}
	for index, argument := range use.call.Args {
		if argument != value {
			continue
		}
		if index == 0 && !use.call.IsInvoke() && use.call.Signature().Recv() != nil {
			site.operation = "receiver"
		}
		sourceIndex := index
		if selector != nil && !use.call.IsInvoke() {
			if selection := c.pass.TypesInfo.Selections[selector]; selection != nil {
				if index == 0 && selection.Kind() == types.MethodExpr {
					site.operation = "receiver"
				}
				if selection.Kind() == types.MethodVal {
					if index == 0 {
						site.operation = "receiver"
						site.pos = selector.X.Pos()
						return site
					}
					sourceIndex-- // SSA includes the implicit receiver before arguments.
				}
			}
		}
		if index == len(use.call.Args)-1 && use.call.Signature().Variadic() && !call.Ellipsis.IsValid() {
			if element, remainder, ok := splitElementPath(path); ok {
				if element < len(call.Args)-sourceIndex {
					sourceIndex += element
					site.path = remainder
					if site.operation == "argument" {
						site.operation = "variadic-argument"
					}
				} else {
					sourceIndex = len(call.Args)
				}
			}
		}
		if sourceIndex < len(call.Args) {
			site.pos = call.Args[sourceIndex].Pos()
			return site
		}
		if len(call.Args) == 1 {
			site.pos = call.Args[0].Pos() // A tuple-producing call supplies several SSA arguments.
			return site
		}
		break
	}
	return site
}

func splitElementPath(path string) (int, string, bool) {
	rest, ok := strings.CutPrefix(path, "[")
	if !ok {
		return 0, "", false
	}
	index, remainder, ok := strings.Cut(rest, "]")
	if !ok {
		return 0, "", false
	}
	element, err := strconv.Atoi(index)
	return element, strings.TrimPrefix(remainder, "."), err == nil && element >= 0
}

func (c *zeroChecker) expressionAt(pos token.Pos) string {
	expression := c.sourceExpression(pos)
	if expression.node != nil && expression.text == "" {
		expression.text = expressionText(c.pass.Fset, expression.node)
		c.expressions[pos] = expression
	}
	return expression.text
}

func (c *zeroChecker) sourceExpression(pos token.Pos) sourceExpression {
	if !pos.IsValid() {
		return sourceExpression{}
	}
	if expression, ok := c.expressions[pos]; ok {
		return expression
	}
	var expression ast.Expr
	for _, file := range c.pass.Files {
		if pos < file.Pos() || pos > file.End() {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil || pos < node.Pos() || pos >= node.End() {
				return false
			}
			if candidate, ok := node.(ast.Expr); ok {
				expression = candidate
			}
			return true
		})
		break
	}
	source := sourceExpression{node: expression}
	c.expressions[pos] = source
	return source
}
