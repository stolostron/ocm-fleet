package domainconstruct

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/diagnostic"
)

func (c *checker) reportProtected(node ast.Node, p *policy, rule, operation, reason, message string) {
	c.pass.Report((diagnostic.Finding{
		Rule: rule, Certainty: diagnostic.Known,
		Operation: operation, Value: expressionText(c.pass.Fset, node), Type: typeIdentity(p.typ),
		Reason: reason, Pos: node.Pos(), Message: message,
	}).Diagnostic())
}

func (c *checker) reportPolicy(node ast.Node, reason, message string) {
	var t types.Type
	if spec, ok := node.(*ast.TypeSpec); ok {
		if object := c.pass.TypesInfo.Defs[spec.Name]; object != nil {
			t = object.Type()
		}
	}
	c.pass.Report((diagnostic.Finding{
		Rule: "domainconstruct.invalid-policy", Certainty: diagnostic.Known,
		Operation: "declaration", Value: expressionText(c.pass.Fset, node), Type: typeIdentity(t),
		Reason: reason, Pos: node.Pos(), Message: message,
	}).Diagnostic())
}

func typeIdentity(t types.Type) string {
	if t == nil {
		return ""
	}
	return types.TypeString(t, nil)
}

func typeName(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Name() })
}

func expressionText(fset *token.FileSet, node ast.Node) string {
	if node == nil {
		return ""
	}
	var text bytes.Buffer
	if err := format.Node(&text, fset, node); err != nil {
		return ""
	}
	return text.String()
}
