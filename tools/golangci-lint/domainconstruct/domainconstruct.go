// Package domainconstruct checks construction and zero-value use of domain values.
package domainconstruct

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"slices"
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
	for _, imported := range pass.AllObjectFacts() {
		object, typeName := imported.Object.(*types.TypeName)
		fact, construction := imported.Fact.(*constructionFact)
		if typeName && construction && c.policies[object] == nil {
			c.policies[object] = &policy{name: object.Pkg().Name() + "." + object.Name(), typ: object.Type(), fact: fact}
		}
	}
	if len(c.policies) == 0 {
		return c.policies, nil
	}
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
	return c.policies, nil
}

func (c *checker) check(node ast.Node, ancestors []ast.Node) bool {
	switch node := node.(type) {
	case *ast.CallExpr:
		if c.pass.TypesInfo.Types[node.Fun].IsType() {
			if !c.checkConversion(node, ancestors) {
				return false
			}
		} else {
			c.checkCallTransfers(node, ancestors)
		}
		if !c.checkGenericConstruction(node, ancestors) {
			return false
		}
	case *ast.CompositeLit:
		if p := c.policy(c.pass.TypesInfo.TypeOf(node)); p != nil &&
			!c.approved(p, ancestors) && !c.zeroCompositeContents(node) {
			c.reportConstruction(node, p)
			return false
		}
		c.checkLiteralTransfers(node, ancestors)
	case *ast.ValueSpec:
		c.checkTransfers(node.Values, len(node.Names), func(index int) types.Type {
			return c.pass.TypesInfo.Defs[node.Names[index]].Type()
		}, ancestors)
	case *ast.ReturnStmt:
		if len(node.Results) > 0 {
			results := c.enclosingResults(ancestors)
			c.checkTransfers(node.Results, results.Len(), func(index int) types.Type {
				return results.At(index).Type()
			}, ancestors)
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
		c.checkTransfers(node.Rhs, len(node.Lhs), func(index int) types.Type {
			return c.pass.TypesInfo.TypeOf(node.Lhs[index])
		}, ancestors)
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
				if target != nil {
					c.checkWrite(target, ancestors)
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
	case *ast.SliceExpr:
		t := c.pass.TypesInfo.TypeOf(node.X)
		if pointer, ok := t.Underlying().(*types.Pointer); ok {
			t = pointer.Elem()
		}
		if _, array := t.Underlying().(*types.Array); array {
			p := c.policy(t)
			if p == nil {
				p = c.fieldOwner(node.X)
			}
			if p != nil && !c.approved(p, ancestors) {
				c.reportProtected(node, p, "domainconstruct.mutable-exposure", "slice", "mutable-contents",
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
			return true
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
	_, boxed := target.Underlying().(*types.Interface)
	if _, pointer := types.Unalias(source).(*types.Pointer); pointer && !boxed {
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
			owner := selection.Recv()
			for _, index := range selection.Index() {
				if pointer, ok := owner.Underlying().(*types.Pointer); ok {
					owner = pointer.Elem()
				}
				if p := c.policy(owner); p != nil {
					return p
				}
				owner = owner.Underlying().(*types.Struct).Field(index).Type()
			}
			return c.fieldOwner(expr.X)
		}
	case *ast.IndexExpr:
		if p := c.policy(c.pass.TypesInfo.TypeOf(expr.X)); p != nil {
			return p
		}
		return c.fieldOwner(expr.X)
	case *ast.StarExpr:
		return c.fieldOwner(expr.X)
	case *ast.SliceExpr:
		return c.fieldOwner(expr.X)
	}
	return nil
}

func (c *checker) approved(p *policy, ancestors []ast.Node) bool {
	if p.functions == nil {
		return false
	}
	for _, ancestor := range slices.Backward(ancestors) {
		switch function := ancestor.(type) {
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
	for _, ancestor := range slices.Backward(ancestors) {
		switch parent := ancestor.(type) {
		case *ast.ParenExpr:
			continue
		case *ast.BinaryExpr:
			switch parent.Op {
			case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
				return true
			}
			if c.pass.TypesInfo.Types[parent].Value != nil {
				expr = parent
				continue
			}
		case *ast.UnaryExpr:
			if c.pass.TypesInfo.Types[parent].Value != nil {
				expr = parent
				continue
			}
		case *ast.CaseClause:
			for _, item := range parent.List {
				if ast.Unparen(item) == ast.Unparen(expr) {
					return true
				}
			}
		}
		break
	}
	return false
}

func (c *checker) isZero(expr ast.Expr) bool {
	expr = ast.Unparen(expr)
	if literal, ok := expr.(*ast.CompositeLit); ok {
		_, address := c.pass.TypesInfo.TypeOf(literal).Underlying().(*types.Pointer)
		return !address && c.zeroCompositeContents(literal)
	}
	value := c.pass.TypesInfo.Types[expr]
	return value.IsNil() || constantZero(value.Value)
}

func (c *checker) zeroCompositeContents(literal *ast.CompositeLit) bool {
	t := c.pass.TypesInfo.TypeOf(literal)
	if pointer, ok := t.Underlying().(*types.Pointer); ok {
		// A pointer element can elide the address operator in a literal.
		t = pointer.Elem()
	}
	switch t.Underlying().(type) {
	case *types.Array, *types.Struct:
		for _, element := range literal.Elts {
			if pair, ok := element.(*ast.KeyValueExpr); ok {
				element = pair.Value
			}
			if !c.isZero(element) {
				return false
			}
		}
		return true
	}
	return false
}

func (c *checker) reportConstruction(node ast.Node, p *policy) {
	c.reportProtected(node, p, "domainconstruct.constructor-bypass", "construction", "constructor-required",
		fmt.Sprintf("construct %s through %s", p.name, strings.Join(p.fact.Constructors, " or ")))
}
