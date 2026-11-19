package domainconstruct

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/exp/typeparams"
)

func (c *checker) checkCallTransfers(call *ast.CallExpr, ancestors []ast.Node) {
	if builtin, ok := expressionObject(c.pass.TypesInfo, call.Fun).(*types.Builtin); ok {
		switch builtin.Name() {
		case "clear", "copy", "delete":
			c.checkWrite(call.Args[0], ancestors)
		}
		return
	}
	signature, ok := c.pass.TypesInfo.TypeOf(call.Fun).Underlying().(*types.Signature)
	if !ok || len(call.Args) == 0 {
		return
	}
	count := len(call.Args)
	if tuple, ok := c.pass.TypesInfo.TypeOf(call.Args[0]).(*types.Tuple); ok {
		count = tuple.Len()
	}
	parameters := signature.Params()
	c.checkTransfers(call.Args, count, func(index int) types.Type {
		if signature.Variadic() && index >= parameters.Len()-1 && !call.Ellipsis.IsValid() {
			return parameters.At(parameters.Len() - 1).Type().Underlying().(*types.Slice).Elem()
		}
		return parameters.At(index).Type()
	}, ancestors)
}

func (c *checker) checkLiteralTransfers(literal *ast.CompositeLit, ancestors []ast.Node) {
	info := c.pass.TypesInfo
	shape := compositeShape(info.TypeOf(literal))
	for index, element := range literal.Elts {
		value := element
		if pair, keyed := element.(*ast.KeyValueExpr); keyed {
			value = pair.Value
			if mapping, ok := shape.(*types.Map); ok {
				c.checkTransfer(pair.Key, info.TypeOf(pair.Key), mapping.Key(), ancestors)
			}
		}
		c.checkTransfer(value, info.TypeOf(value), c.literalElementType(shape, element, index), ancestors)
	}
}

func (c *checker) checkRangeTransfers(statement *ast.RangeStmt, ancestors []ast.Node) {
	key, value := rangeTypes(c.pass.TypesInfo.TypeOf(statement.X))
	for _, binding := range []struct {
		target ast.Expr
		source types.Type
	}{{statement.Key, key}, {statement.Value, value}} {
		if binding.target != nil && c.checkWrite(binding.target, ancestors) {
			c.checkTransfer(binding.target, binding.source, c.pass.TypesInfo.TypeOf(binding.target), ancestors)
		}
	}
}

func (c *checker) checkTransfers(values []ast.Expr, count int, target func(int) types.Type, ancestors []ast.Node) {
	if len(values) == 1 {
		if tuple, ok := c.pass.TypesInfo.TypeOf(values[0]).(*types.Tuple); ok {
			for index := range min(count, tuple.Len()) {
				c.checkTransfer(values[0], tuple.At(index).Type(), target(index), ancestors)
			}
			return
		}
	}
	for index, value := range values {
		c.checkTransfer(value, c.pass.TypesInfo.TypeOf(value), target(index), ancestors)
	}
}

func (c *checker) checkTransfer(value ast.Expr, source, target types.Type, ancestors []ast.Node) {
	p := c.policy(target)
	if p == nil || source == nil || types.Identical(source, target) || !types.Identical(source.Underlying(), target.Underlying()) {
		return
	}
	// Contextual scalar constants already carry the destination type. Here
	// Go kept an unnamed representation while assigning it to a named type.
	if _, named := types.Unalias(source).(*types.Named); !named && !c.isZero(value) && !c.approved(p, ancestors) {
		c.reportConstruction(value, p)
	}
}

func (c *checker) enclosingResults(ancestors []ast.Node) *types.Tuple {
	for _, parent := range slices.Backward(ancestors) {
		switch function := parent.(type) {
		case *ast.FuncDecl:
			return c.pass.TypesInfo.Defs[function.Name].Type().(*types.Signature).Results()
		case *ast.FuncLit:
			return c.pass.TypesInfo.TypeOf(function).(*types.Signature).Results()
		}
	}
	return nil
}

// A pointer element's literal can elide its address operator.
func compositeShape(t types.Type) types.Type {
	shape := t.Underlying()
	if pointer, ok := shape.(*types.Pointer); ok {
		return pointer.Elem().Underlying()
	}
	return shape
}

func (c *checker) literalElementType(shape types.Type, element ast.Expr, index int) types.Type {
	switch shape := shape.(type) {
	case *types.Struct:
		if pair, keyed := element.(*ast.KeyValueExpr); keyed {
			return c.pass.TypesInfo.TypeOf(pair.Key)
		}
		return shape.Field(index).Type()
	case *types.Slice:
		return shape.Elem()
	case *types.Array:
		return shape.Elem()
	case *types.Map:
		return shape.Elem()
	}
	return nil
}

func rangeTypes(t types.Type) (key, value types.Type) {
	shape := rangeUnderlying(t)
	if pointer, ok := shape.(*types.Pointer); ok {
		shape = pointer.Elem().Underlying()
	}
	switch shape := shape.(type) {
	case *types.Array:
		return types.Typ[types.Int], shape.Elem()
	case *types.Slice:
		return types.Typ[types.Int], shape.Elem()
	case *types.Map:
		return shape.Key(), shape.Elem()
	case *types.Chan:
		return shape.Elem(), nil
	case *types.Basic:
		if shape.Info()&types.IsString != 0 {
			return types.Typ[types.Int], types.Typ[types.Rune]
		}
		return t, nil
	case *types.Signature:
		yield, ok := rangeUnderlying(shape.Params().At(0).Type()).(*types.Signature)
		if !ok {
			return nil, nil
		}
		params := yield.Params()
		switch params.Len() {
		case 1:
			return params.At(0).Type(), nil
		case 2:
			return params.At(0).Type(), params.At(1).Type()
		}
	}
	return nil, nil
}

func rangeUnderlying(t types.Type) types.Type {
	if parameter, ok := types.Unalias(t).(*types.TypeParam); ok {
		terms, err := typeparams.NormalTerms(parameter)
		if err != nil || len(terms) == 0 {
			return nil
		}
		// The type checker requires a common shape for the range operand and yield.
		return terms[0].Type().Underlying()
	}
	return t.Underlying()
}
