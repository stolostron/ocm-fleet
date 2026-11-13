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

// Resolve source positions only for findings that survive deduplication. The
// expression cache also serves diagnostic text, without indexing other calls.
func (c *zeroChecker) usePosition(use zeroUse, value ssa.Value, path string) token.Pos {
	if use.call == nil {
		return use.pos
	}
	call, ok := c.sourceExpression(use.call.Pos()).node.(*ast.CallExpr)
	if !ok {
		return use.pos
	}
	selector, _ := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if value == use.call.Value {
		if use.call.IsInvoke() && selector != nil {
			return selector.X.Pos()
		}
		return call.Fun.Pos()
	}
	for index, argument := range use.call.Args {
		if argument != value {
			continue
		}
		sourceIndex := index
		if selector != nil && !use.call.IsInvoke() {
			if selection := c.pass.TypesInfo.Selections[selector]; selection != nil && selection.Kind() == types.MethodVal {
				if index == 0 {
					return selector.X.Pos()
				}
				sourceIndex-- // SSA includes the implicit receiver before arguments.
			}
		}
		if index == len(use.call.Args)-1 && use.call.Signature().Variadic() && !call.Ellipsis.IsValid() {
			if element, ok := leadingElementIndex(path); ok {
				if element < len(call.Args)-sourceIndex {
					sourceIndex += element
				} else {
					sourceIndex = len(call.Args)
				}
			}
		}
		if sourceIndex < len(call.Args) {
			return call.Args[sourceIndex].Pos()
		}
		if len(call.Args) == 1 {
			return call.Args[0].Pos() // A tuple-producing call supplies several SSA arguments.
		}
		break
	}
	return use.pos
}

func leadingElementIndex(path string) (int, bool) {
	rest, ok := strings.CutPrefix(path, "[")
	if !ok {
		return 0, false
	}
	index, _, ok := strings.Cut(rest, "]")
	if !ok {
		return 0, false
	}
	element, err := strconv.Atoi(index)
	return element, err == nil && element >= 0
}
