package domainconstruct

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// A source reference separates an expression from a description of storage
// that has no source variable. Descriptions belong in provenance, never Value.
type sourceReference struct {
	expression  string
	description string
}

func (reference sourceReference) describe(fallback string) string {
	if reference.description != "" {
		return reference.description
	}
	if reference.expression != "" {
		return reference.expression
	}
	return fallback
}

func (c *zeroChecker) valueName(value ssa.Value, path []int) string {
	return c.valueReference(value, path).expression
}

func (c *zeroChecker) storageName(value ssa.Value, path []int) string {
	return c.valueReference(value, path).describe("unnamed storage")
}

func (c *zeroChecker) valueReference(value ssa.Value, path []int) sourceReference {
	if value == nil {
		return sourceReference{}
	}
	var reference sourceReference
	switch value := value.(type) {
	case *ssa.Parameter:
		if object := value.Object(); object.Pos().IsValid() {
			reference.expression = object.Name()
		}
	case *ssa.FreeVar:
		// Lexical captures retain source names. A generated method wrapper's
		// receiver does not, even though SSA also represents it as a capture.
		if _, lexical := value.Parent().Syntax().(*ast.FuncLit); lexical {
			reference.expression = value.Name()
		}
	case *ssa.Const:
		// Aggregate zero constants also have a nil payload. They have neither
		// a source position nor a nil expression that could name their members.
		if len(path) == 0 {
			if value.IsNil() {
				reference.expression = "nil"
			} else if value.Value != nil {
				reference.expression = value.Value.ExactString()
			}
		}
		return reference
	case *ssa.Extract:
		reference = c.valueReference(value.Tuple, nil)
	default:
		source := c.sourceExpression(value.Pos()).node
		if allocation, ok := value.(*ssa.Alloc); ok && !c.sourceAllocation(allocation, source) {
			break
		}
		reference.expression = c.expressionAt(value.Pos())
		switch source.(type) {
		case *ast.CompositeLit:
			reference.description = "composite literal"
		case *ast.BinaryExpr, *ast.UnaryExpr, *ast.StarExpr:
			if len(path) != 0 {
				reference.expression = "(" + reference.expression + ")"
			}
		}
	}
	return reference.project(value.Type(), path)
}

// Allocation comments are compiler hints, not a source-name contract. Resolve
// real declarations and explicit allocations from syntax instead; a variadic
// packing allocation, for example, has no corresponding source expression.
func (c *zeroChecker) sourceAllocation(allocation *ssa.Alloc, expression ast.Expr) bool {
	switch expression := expression.(type) {
	case *ast.Ident:
		if c.pass.TypesInfo != nil {
			variable, ok := c.pass.TypesInfo.Defs[expression].(*types.Var)
			return ok && variable.Pos() == allocation.Pos()
		}
	case *ast.CompositeLit:
		return true
	case *ast.CallExpr:
		identifier, ok := ast.Unparen(expression.Fun).(*ast.Ident)
		if ok && c.pass.TypesInfo != nil {
			builtin, ok := c.pass.TypesInfo.Uses[identifier].(*types.Builtin)
			return ok && (builtin.Name() == "new" || builtin.Name() == "make")
		}
	}
	return false
}

func (reference sourceReference) project(t types.Type, path []int) sourceReference {
	// Member paths share the analysis's field and element indices. A symbolic
	// index can describe a location, but cannot form a known source expression.
	canNameMember := reference.expression != "" && reference.description == ""
	var suffix strings.Builder
	for _, index := range path {
		if pointer, ok := t.Underlying().(*types.Pointer); ok {
			t = pointer.Elem()
		}
		switch parent := t.Underlying().(type) {
		case *types.Struct:
			if index < 0 || index >= parent.NumFields() {
				return reference
			}
			field := parent.Field(index)
			suffix.WriteString("." + field.Name())
			t = field.Type()
		case *types.Array:
			if index >= int(parent.Len()) {
				return reference
			}
			suffix.WriteString(elementLabel(int64(index)))
			canNameMember = canNameMember && index >= 0
			t = parent.Elem()
		case *types.Slice:
			suffix.WriteString(elementLabel(int64(index)))
			canNameMember = canNameMember && index >= 0
			t = parent.Elem()
		default:
			return reference
		}
	}
	if suffix.Len() == 0 {
		return reference
	}
	if canNameMember {
		reference.expression += suffix.String()
	} else {
		reference.description = reference.describe("unnamed storage") + " at path " + strings.TrimPrefix(suffix.String(), ".")
	}
	return reference
}

func (c *zeroChecker) callName(call *ssa.CallCommon) string {
	function := call.StaticCallee()
	if function != nil {
		if object := function.Object(); object != nil {
			return object.Name()
		}
	}
	if call.Method != nil {
		return call.Method.Name()
	}
	if expression, ok := c.sourceExpression(call.Pos()).node.(*ast.CallExpr); ok {
		if _, literal := ast.Unparen(expression.Fun).(*ast.FuncLit); literal {
			return "anonymous function"
		}
		return expressionText(c.pass.Fset, expression.Fun)
	}
	if function != nil {
		if _, literal := function.Syntax().(*ast.FuncLit); literal {
			return "anonymous function"
		}
	}
	return c.valueReference(call.Value, nil).describe("call")
}
