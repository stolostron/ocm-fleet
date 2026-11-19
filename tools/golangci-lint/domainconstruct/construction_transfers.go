package domainconstruct

import (
	"go/ast"
	"go/types"
	"slices"
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
	shape := info.TypeOf(literal).Underlying()
	for index, element := range literal.Elts {
		value := element
		pair, keyed := element.(*ast.KeyValueExpr)
		if keyed {
			value = pair.Value
		}
		var target types.Type
		switch shape := shape.(type) {
		case *types.Struct:
			if keyed {
				target = info.TypeOf(pair.Key)
			} else {
				target = shape.Field(index).Type()
			}
		case *types.Slice:
			target = shape.Elem()
		case *types.Array:
			target = shape.Elem()
		case *types.Map:
			c.checkTransfer(pair.Key, info.TypeOf(pair.Key), shape.Key(), ancestors)
			target = shape.Elem()
		}
		c.checkTransfer(value, info.TypeOf(value), target, ancestors)
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
