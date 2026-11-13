// Package domainconstruct checks construction and zero-value use of domain values.
package domainconstruct

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer requires approved construction for annotated domain types.
var Analyzer = &analysis.Analyzer{
	Name:       "domainconstruct",
	Doc:        "require constructors for annotated domain values",
	Run:        run,
	FactTypes:  []analysis.Fact{new(constructionFact)},
	ResultType: reflect.TypeFor[policySet](),
	Requires:   []*analysis.Analyzer{contractsAnalyzer},
}

type checker struct {
	pass      *analysis.Pass
	policies  policySet
	contracts *contractSet
}

func run(pass *analysis.Pass) (any, error) {
	c := &checker{pass: pass, policies: make(policySet), contracts: pass.ResultOf[contractsAnalyzer].(*contractSet)}
	c.collectPolicies()
	for _, file := range pass.Files {
		var ancestors []ast.Node
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				ancestors = ancestors[:len(ancestors)-1]
				return true
			}
			if !c.check(node, ancestors) {
				return false
			}
			ancestors = append(ancestors, node)
			return true
		})
	}
	for _, imported := range pass.AllObjectFacts() {
		object, typeName := imported.Object.(*types.TypeName)
		fact, construction := imported.Fact.(*constructionFact)
		if typeName && construction && c.policies[object] == nil {
			c.policies[object] = &policy{name: object.Pkg().Name() + "." + object.Name(), typ: object.Type(), fact: fact}
		}
	}
	return c.policies, nil
}

func (c *checker) check(node ast.Node, ancestors []ast.Node) bool {
	switch node := node.(type) {
	case *ast.CallExpr:
		if c.pass.TypesInfo.Types[node.Fun].IsType() && !c.checkConversion(node, ancestors) {
			return false
		}
		if !c.checkGenericConstruction(node, ancestors) {
			return false
		}
	case *ast.CompositeLit:
		if p := c.policy(c.pass.TypesInfo.TypeOf(node)); p != nil &&
			!c.approved(p, ancestors) && !c.isZero(node) {
			c.reportConstruction(node, p)
			return false
		}
	case *ast.AssignStmt:
		for _, target := range node.Lhs {
			if !c.checkWrite(target, ancestors) {
				return false
			}
			if node.Tok != token.ASSIGN && node.Tok != token.DEFINE {
				if p := c.policy(c.pass.TypesInfo.TypeOf(target)); p != nil && !c.approved(p, ancestors) {
					c.reportConstruction(node, p)
					return false
				}
			}
		}
	case *ast.IncDecStmt:
		if !c.checkWrite(node.X, ancestors) {
			return false
		}
		if p := c.policy(c.pass.TypesInfo.TypeOf(node.X)); p != nil && !c.approved(p, ancestors) {
			c.reportConstruction(node, p)
			return false
		}
	case *ast.RangeStmt:
		if node.Tok == token.ASSIGN {
			for _, target := range []ast.Expr{node.Key, node.Value} {
				if target != nil && !c.checkWrite(target, ancestors) {
					return false
				}
			}
		}
	case *ast.UnaryExpr:
		if node.Op == token.AND {
			if p := c.fieldOwner(node.X); p != nil && !c.approved(p, ancestors) {
				c.reportProtected(node, p, "domainconstruct.mutable-exposure", "address", "mutable-contents",
					fmt.Sprintf("do not expose mutable contents of %s", p.name))
				return false
			}
		}
	}

	expr, ok := node.(ast.Expr)
	if !ok {
		return true
	}
	p := c.policy(c.pass.TypesInfo.TypeOf(expr))
	if p == nil || c.approved(p, ancestors) {
		return true
	}
	if c.pass.TypesInfo.Types[expr].Value != nil && !c.constantCopy(expr) {
		if c.constantObservation(expr, ancestors) {
			return false
		}
		if !c.isZero(expr) {
			c.reportConstruction(expr, p)
			return false
		}
		return true
	}
	switch expr := expr.(type) {
	case *ast.BinaryExpr:
		c.reportConstruction(expr, p)
		return false
	case *ast.UnaryExpr:
		if expr.Op == token.ADD || expr.Op == token.SUB || expr.Op == token.XOR || expr.Op == token.NOT {
			c.reportConstruction(expr, p)
			return false
		}
	}
	return true
}

func (c *checker) checkConversion(call *ast.CallExpr, ancestors []ast.Node) bool {
	if len(call.Args) != 1 {
		return true
	}
	target, source := c.pass.TypesInfo.TypeOf(call), c.pass.TypesInfo.TypeOf(call.Args[0])
	if types.Identical(target, source) {
		return true
	}
	if _, pointer := types.Unalias(target).(*types.Pointer); pointer && c.pass.TypesInfo.Types[call.Args[0]].IsNil() {
		return true
	}
	if p := c.policy(target); p != nil && !c.approved(p, ancestors) &&
		!c.isZero(call) {
		c.reportConstruction(call, p)
		return false
	}
	if _, pointer := types.Unalias(source).(*types.Pointer); pointer {
		if p := c.policy(source); p != nil && !c.approved(p, ancestors) {
			c.reportProtected(call, p, "domainconstruct.mutable-exposure", "conversion", "mutable-representation",
				fmt.Sprintf("do not expose mutable representation of %s", p.name))
			return false
		}
	}
	return true
}

func (c *checker) checkWrite(target ast.Expr, ancestors []ast.Node) bool {
	if p := c.fieldOwner(target); p != nil && !c.approved(p, ancestors) {
		c.reportProtected(target, p, "domainconstruct.field-write", "assignment", "protected-field",
			fmt.Sprintf("modify %s through %s", p.name, strings.Join(p.fact.Constructors, " or ")))
		return false
	}
	return true
}

func (c *checker) fieldOwner(expr ast.Expr) *policy {
	switch expr := ast.Unparen(expr).(type) {
	case *ast.SelectorExpr:
		if selection := c.pass.TypesInfo.Selections[expr]; selection != nil && selection.Kind() == types.FieldVal {
			if p := c.policy(c.pass.TypesInfo.TypeOf(expr.X)); p != nil {
				return p
			}
			return c.fieldOwner(expr.X)
		}
	case *ast.IndexExpr:
		return c.fieldOwner(expr.X)
	case *ast.StarExpr:
		return c.fieldOwner(expr.X)
	}
	return nil
}

func (c *checker) approved(p *policy, ancestors []ast.Node) bool {
	if p.functions == nil {
		return false
	}
	for i := len(ancestors) - 1; i >= 0; i-- {
		switch function := ancestors[i].(type) {
		case *ast.FuncLit:
			// A returned closure must not inherit permission to manufacture values.
			return false
		case *ast.FuncDecl:
			fn, _ := c.pass.TypesInfo.Defs[function.Name].(*types.Func)
			return p.functions[fn]
		}
	}
	return false
}

func (c *checker) constantCopy(expr ast.Expr) bool {
	constant, ok := expressionObject(c.pass.TypesInfo, expr).(*types.Const)
	return ok && types.Identical(constant.Type(), c.pass.TypesInfo.TypeOf(expr))
}

// A contextual literal in a comparison observes a representation. An explicit
// conversion still requests construction and is checked before reaching here.
func (c *checker) constantObservation(expr ast.Expr, ancestors []ast.Node) bool {
	if _, conversion := ast.Unparen(expr).(*ast.CallExpr); conversion {
		return false
	}
	for i := len(ancestors) - 1; i >= 0; i-- {
		switch parent := ancestors[i].(type) {
		case *ast.ParenExpr:
			continue
		case *ast.BinaryExpr:
			switch parent.Op {
			case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
				return true
			}
		case *ast.CaseClause:
			for _, item := range parent.List {
				if ast.Unparen(item) == expr {
					return true
				}
			}
		}
		break
	}
	return false
}

func (c *checker) isZero(expr ast.Expr) bool {
	if literal, ok := ast.Unparen(expr).(*ast.CompositeLit); ok {
		return len(literal.Elts) == 0
	}
	return constantZero(c.pass.TypesInfo.Types[expr].Value)
}

func (c *checker) reportConstruction(node ast.Node, p *policy) {
	c.reportProtected(node, p, "domainconstruct.constructor-bypass", "construction", "constructor-required",
		fmt.Sprintf("construct %s through %s", p.name, strings.Join(p.fact.Constructors, " or ")))
}
