package domainconstruct

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/exp/typeparams"
)

func (c *checker) checkCallTransfers(call *ast.CallExpr, ancestors []ast.Node) {
	if builtin, ok := expressionObject(c.pass.TypesInfo, call.Fun).(*types.Builtin); ok {
		switch builtin.Name() {
		case "clear", "copy", "delete":
			c.checkWrite(call.Args[0], ancestors)
			return
		case "append":
			if c.appendMayWrite(call) {
				c.checkWrite(call.Args[0], ancestors)
			}
			if call.Ellipsis.IsValid() {
				return // A spread already has the destination element type, or appends string bytes.
			}
		default:
			return
		}
	}
	signature, ok := operandUnderlying(c.pass.TypesInfo.TypeOf(call.Fun)).(*types.Signature)
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

func (c *checker) appendMayWrite(call *ast.CallExpr) bool {
	if len(call.Args) < 2 {
		return false // A tuple has no directly selected destination.
	}
	info := c.pass.TypesInfo
	if view, ok := ast.Unparen(call.Args[0]).(*ast.SliceExpr); ok && view.Max != nil {
		high, max := info.Types[view.High].Value, info.Types[view.Max].Value
		if high != nil && max != nil {
			if constant.Compare(high, token.EQL, max) {
				return false
			}
			if !call.Ellipsis.IsValid() {
				added := constant.MakeInt64(int64(len(call.Args) - 1))
				spare := constant.BinaryOp(max, token.SUB, high)
				if constant.Compare(added, token.GTR, spare) {
					return false
				}
			}
		}
		highName, highIdent := ast.Unparen(view.High).(*ast.Ident)
		maxName, maxIdent := ast.Unparen(view.Max).(*ast.Ident)
		if highIdent && maxIdent && info.ObjectOf(highName) == info.ObjectOf(maxName) {
			return false
		}
	}
	if call.Ellipsis.IsValid() {
		values := ast.Unparen(call.Args[1])
		if literal, ok := values.(*ast.CompositeLit); ok && len(literal.Elts) == 0 {
			return false
		}
		return !c.isZero(values)
	}
	return true
}

func (c *checker) checkSend(statement *ast.SendStmt, ancestors []ast.Node) {
	info := c.pass.TypesInfo
	if channel, ok := operandUnderlying(info.TypeOf(statement.Chan)).(*types.Chan); ok {
		c.checkTransfer(statement.Value, info.TypeOf(statement.Value), channel.Elem(), ancestors)
	}
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

func (c *checker) checkReturnedStorage(value ast.Expr, ancestors []ast.Node) {
	if base := c.mutableStorageBase(value); base != nil {
		if p := c.unapprovedOwner(base, ancestors); p != nil {
			c.reportProtected(value, p, "domainconstruct.mutable-exposure", "return", "mutable-contents",
				"do not expose mutable contents of "+p.name)
		}
	}
}

func (c *checker) mutableStorageBase(value ast.Expr) ast.Expr {
	info := c.pass.TypesInfo
	switch operandUnderlying(info.TypeOf(value)).(type) {
	case *types.Slice, *types.Map:
	default:
		return nil
	}
	base := ast.Unparen(value)
	for {
		view, ok := base.(*ast.SliceExpr)
		if !ok {
			break
		}
		if _, slice := info.TypeOf(view.X).Underlying().(*types.Slice); !slice {
			return nil // Slicing a protected array is already checked at the expression.
		}
		if view.Max != nil {
			low := constant.MakeInt64(0)
			if view.Low != nil {
				low = info.Types[view.Low].Value
			}
			if limit := info.Types[view.Max].Value; low != nil && limit != nil && constant.Compare(low, token.EQL, limit) {
				return nil // Nothing in the backing storage is accessible through this view.
			}
		}
		base = ast.Unparen(view.X)
	}
	return base
}

func (c *checker) checkRangeTransfers(statement *ast.RangeStmt, ancestors []ast.Node) {
	info := c.pass.TypesInfo
	iterated := info.TypeOf(statement.X)
	if basic, integer := operandUnderlying(iterated).(*types.Basic); integer && basic.Info()&types.IsInteger != 0 {
		if bound := info.Types[statement.X].Value; bound != nil && constant.Sign(bound) <= 0 {
			return
		}
		if key := statement.Key; key != nil && (statement.Tok != token.ASSIGN || c.checkAssignment(key, ancestors)) {
			// Integer ranges synthesize values even when Go gives the binding
			// the same type as the bound. Other ranges transfer existing values.
			if p := c.policy(info.TypeOf(key)); p != nil && !c.approved(p, ancestors) {
				c.reportConstruction(key, p)
			}
		}
		return
	}
	if statement.Tok != token.ASSIGN {
		return
	}
	key, value := rangeTypes(iterated)
	for _, binding := range []struct {
		target ast.Expr
		source types.Type
	}{{statement.Key, key}, {statement.Value, value}} {
		if binding.target != nil && c.checkAssignment(binding.target, ancestors) {
			c.checkTransfer(binding.target, binding.source, info.TypeOf(binding.target), ancestors)
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
	source, target = exactOperandType(source), exactOperandType(target)
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
	shape := operandUnderlying(t)
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
	shape := operandUnderlying(t)
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
		yield, ok := operandUnderlying(shape.Params().At(0).Type()).(*types.Signature)
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

func operandUnderlying(t types.Type) types.Type {
	if parameter, ok := types.Unalias(t).(*types.TypeParam); ok {
		terms, err := typeparams.NormalTerms(parameter)
		if err != nil || len(terms) == 0 {
			return nil
		}
		// The type checker requires a common shape for these operations.
		return terms[0].Type().Underlying()
	}
	return t.Underlying()
}

// An exact constraint retains the concrete operand's domain identity.
func exactOperandType(t types.Type) types.Type {
	if parameter, ok := types.Unalias(t).(*types.TypeParam); ok {
		if terms, err := typeparams.NormalTerms(parameter); err == nil && len(terms) == 1 && !terms[0].Tilde() {
			return terms[0].Type()
		}
	}
	return t
}
