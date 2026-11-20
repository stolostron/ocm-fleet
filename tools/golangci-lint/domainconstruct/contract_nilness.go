package domainconstruct

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
)

type nilnessEnv map[*types.Var]truth

// A local AST proof of a boxed zero depends on one function type parameter.
// SSA flow only sees the result after the caller supplies a concrete type.
const boxedTypeZero truth = notZero + 1

type nilnessEvaluator struct {
	builder       *contractBuilder
	signature     *types.Signature
	result        int
	returns       []truth
	boxedZeroType int
	complete      bool
	work          *workBudget
}

func (e *nilnessEvaluator) function(body *ast.BlockStmt, initial nilnessEnv) truth {
	e.returns, e.complete = nil, true
	e.boxedZeroType = 0
	work := workBudget(maxInferenceWork)
	e.work = &work
	environment := maps.Clone(initial)
	if environment == nil {
		environment = make(nilnessEnv)
	}
	for result := range e.signature.Results().Variables() {
		environment[result] = isZero
	}
	remaining := e.block(body.List, []nilnessEnv{environment})
	if !e.complete || len(remaining) != 0 || len(e.returns) == 0 {
		return unknown
	}
	state := e.returns[0]
	for _, result := range e.returns[1:] {
		if result != state {
			return unknown
		}
	}
	return state
}

func (e *nilnessEvaluator) block(statements []ast.Stmt, states []nilnessEnv) []nilnessEnv {
	for _, statement := range statements {
		var next []nilnessEnv
		for _, state := range states {
			next = append(next, e.statement(statement, state)...)
		}
		states = next
		if !e.complete || len(states) == 0 {
			break
		}
		if len(states) > 32 {
			e.complete = false
			break
		}
	}
	return states
}

func (e *nilnessEvaluator) statement(statement ast.Stmt, state nilnessEnv) []nilnessEnv {
	ast.Inspect(statement, func(node ast.Node) bool {
		if node != nil && (!e.complete || !e.work.take()) {
			e.complete = false
			return false
		}
		if _, closure := node.(*ast.FuncLit); closure {
			e.complete = false
		}
		if unary, ok := node.(*ast.UnaryExpr); ok && unary.Op == token.AND {
			if _, variable := expressionObject(e.builder.pass.TypesInfo, unary.X).(*types.Var); variable {
				e.complete = false
			}
		}
		return true
	})
	if !e.complete {
		return nil
	}
	switch statement := statement.(type) {
	case *ast.BlockStmt:
		return e.block(statement.List, []nilnessEnv{state})
	case *ast.ReturnStmt:
		result := unknown
		if len(statement.Results) == 0 {
			result = state[e.signature.Results().At(e.result)]
		} else if len(statement.Results) == e.signature.Results().Len() {
			result = e.converted(statement.Results[e.result], e.signature.Results().At(e.result).Type(), state)
		} else if call, ok := ast.Unparen(statement.Results[0]).(*ast.CallExpr); ok {
			actual := e.builder.pass.TypesInfo.TypeOf(call).(*types.Tuple).At(e.result).Type()
			result = e.convertedResult(call, e.result, actual, e.signature.Results().At(e.result).Type(), state)
		}
		e.returns = append(e.returns, result)
		return nil
	case *ast.IfStmt:
		if statement.Init != nil {
			if next := e.statement(statement.Init, state); len(next) != 1 {
				e.complete = false
				return nil
			}
		}
		condition := e.condition(statement.Cond, state)
		var next []nilnessEnv
		if condition != isZero {
			branch := maps.Clone(state)
			e.refine(statement.Cond, notZero, branch)
			next = append(next, e.block(statement.Body.List, []nilnessEnv{branch})...)
		}
		if condition != notZero {
			branch := maps.Clone(state)
			e.refine(statement.Cond, isZero, branch)
			if statement.Else != nil {
				next = append(next, e.statement(statement.Else, branch)...)
			} else {
				next = append(next, branch)
			}
		}
		return next
	case *ast.AssignStmt:
		var values []truth
		for index, target := range statement.Lhs {
			value := unknown
			if len(statement.Rhs) == len(statement.Lhs) {
				value = e.converted(statement.Rhs[index], e.builder.pass.TypesInfo.TypeOf(target), state)
			} else if len(statement.Rhs) == 1 {
				if call, ok := ast.Unparen(statement.Rhs[0]).(*ast.CallExpr); ok {
					actual := e.builder.pass.TypesInfo.TypeOf(call).(*types.Tuple).At(index).Type()
					value = e.convertedResult(call, index, actual, e.builder.pass.TypesInfo.TypeOf(target), state)
				}
			}
			values = append(values, value)
		}
		for index, target := range statement.Lhs {
			if _, identifier := ast.Unparen(target).(*ast.Ident); !identifier {
				e.complete = false
				continue
			}
			if variable, ok := expressionObject(e.builder.pass.TypesInfo, target).(*types.Var); ok {
				if variable.Pkg() != nil && variable.Parent() == variable.Pkg().Scope() {
					e.complete = false
				}
				state[variable] = values[index]
			}
		}
	case *ast.DeclStmt:
		declaration, ok := statement.Decl.(*ast.GenDecl)
		if !ok || declaration.Tok != token.VAR {
			e.complete = false
			break
		}
		for _, spec := range declaration.Specs {
			spec := spec.(*ast.ValueSpec)
			for index, name := range spec.Names {
				variable := e.builder.pass.TypesInfo.Defs[name].(*types.Var)
				state[variable] = isZero
				if len(spec.Values) == len(spec.Names) {
					state[variable] = e.converted(spec.Values[index], variable.Type(), state)
				} else if len(spec.Values) != 0 {
					e.complete = false
				}
			}
		}
	case *ast.ExprStmt:
		if call, ok := statement.X.(*ast.CallExpr); ok {
			if object, ok := expressionObject(e.builder.pass.TypesInfo, call.Fun).(*types.Builtin); ok && object.Name() == "panic" {
				return nil
			}
		}
	case *ast.EmptyStmt:
	default:
		e.complete = false
	}
	return []nilnessEnv{state}
}

func (e *nilnessEvaluator) converted(expression ast.Expr, target types.Type, state nilnessEnv) truth {
	actual := e.builder.pass.TypesInfo.TypeOf(expression)
	if _, tuple := actual.(*types.Tuple); tuple {
		return unknown
	}
	if target != nil {
		if basic, ok := target.Underlying().(*types.Basic); ok && basic.Info()&types.IsBoolean != 0 {
			return e.condition(expression, state)
		}
	}
	if boxedByConversion(actual, target) {
		return notZero
	}
	return e.conversionTruth(actual, target, e.expression(expression, state))
}

func boxedByConversion(actual, target types.Type) bool {
	if actual == nil || actual == types.Typ[types.UntypedNil] || !interfaceValueType(target) {
		return false
	}
	_, parameter := types.Unalias(actual).(*types.TypeParam)
	return !parameter && !interfaceValueType(actual)
}

func interfaceValueType(t types.Type) bool {
	if t == nil {
		return false
	}
	if _, parameter := types.Unalias(t).(*types.TypeParam); parameter {
		return false
	}
	_, ok := t.Underlying().(*types.Interface)
	return ok
}

func (e *nilnessEvaluator) convertedResult(call *ast.CallExpr, index int, actual, target types.Type, state nilnessEnv) truth {
	if boxedByConversion(actual, target) {
		return notZero
	}
	return e.conversionTruth(actual, target, e.call(call, index, state))
}

func (e *nilnessEvaluator) conversionTruth(actual, target types.Type, value truth) truth {
	parameter, generic := types.Unalias(actual).(*types.TypeParam)
	if !generic || !interfaceValueType(target) || value == notZero {
		return value
	}
	if value == isZero && e.signature != nil {
		index := parameter.Index()
		parameters := e.signature.TypeParams()
		if index >= 0 && index < parameters.Len() && parameters.At(index) == parameter && (e.boxedZeroType == 0 || e.boxedZeroType == index+1) {
			e.boxedZeroType = index + 1
			return boxedTypeZero
		}
	}
	return unknown
}

func (e *nilnessEvaluator) expression(expression ast.Expr, state nilnessEnv) truth {
	if !e.work.take() {
		e.complete = false
		return unknown
	}
	expression = ast.Unparen(expression)
	if e.builder.pass.TypesInfo.Types[expression].IsNil() {
		return isZero
	}
	switch expression := expression.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		if variable, ok := expressionObject(e.builder.pass.TypesInfo, expression).(*types.Var); ok {
			if value := state[variable]; value != unknown && !variable.IsField() {
				return value
			}
			if variable.Pkg() != nil && variable.Parent() == variable.Pkg().Scope() && e.builder.sentinel(variable) {
				return notZero
			}
		}
	case *ast.UnaryExpr:
		if expression.Op == token.AND {
			return notZero
		}
	case *ast.CallExpr:
		return e.call(expression, 0, state)
	}
	return unknown
}

func (e *nilnessEvaluator) call(call *ast.CallExpr, result int, state nilnessEnv) truth {
	if !e.work.take() {
		e.complete = false
		return unknown
	}
	if e.builder.pass.TypesInfo.Types[call.Fun].IsType() && len(call.Args) == 1 {
		return e.converted(call.Args[0], e.builder.pass.TypesInfo.TypeOf(call), state)
	}
	object := expressionObject(e.builder.pass.TypesInfo, call.Fun)
	if builtin, ok := object.(*types.Builtin); ok && builtin.Name() == "new" {
		return notZero
	}
	function, ok := object.(*types.Func)
	if !ok {
		return unknown
	}
	if function.Pkg() != nil {
		path, name := function.Pkg().Path(), function.Name()
		if path == "errors" && name == "New" || path == "fmt" && name == "Errorf" {
			return notZero
		}
		if path == "errors" && name == "Join" && !call.Ellipsis.IsValid() {
			joined := isZero
			for _, argument := range call.Args {
				if _, tuple := e.builder.pass.TypesInfo.TypeOf(argument).(*types.Tuple); tuple {
					return unknown
				}
				switch e.converted(argument, types.Universe.Lookup("error").Type(), state) {
				case notZero:
					return notZero
				case unknown:
					joined = unknown
				case boxedTypeZero:
					if joined == isZero {
						joined = boxedTypeZero
					}
				}
			}
			return joined
		}
	}
	if fact := e.builder.function(function); fact != nil {
		for _, rule := range fact.Errors {
			if rule.Result != result {
				continue
			}
			value := unknown
			if rule.Parameter >= 0 {
				argument := callParameter(e.builder.pass.TypesInfo, call, rule.Parameter)
				if argument.expression == nil {
					continue
				}
				if argument.result < 0 {
					value = e.converted(argument.expression, argument.formal, state)
				} else if spread, ok := ast.Unparen(argument.expression).(*ast.CallExpr); ok {
					value = e.convertedResult(spread, argument.result, argument.actual, argument.formal, state)
				}
			}
			var typeArgument types.Type
			if rule.BoxedZeroType != 0 {
				instance := e.builder.pass.TypesInfo.Instances[calledIdentifier(ast.Unparen(call.Fun))]
				if rule.BoxedZeroType <= instance.TypeArgs.Len() {
					typeArgument = instance.TypeArgs.At(rule.BoxedZeroType - 1)
				}
			}
			if state := rule.resultTruth(value, typeArgument); state != unknown {
				return state
			}
		}
	}
	return unknown
}

func (e *nilnessEvaluator) condition(expression ast.Expr, state nilnessEnv) truth {
	if literal := e.builder.pass.TypesInfo.Types[expression].Value; literal != nil && literal.Kind() == constant.Bool {
		if constant.BoolVal(literal) {
			return notZero
		}
		return isZero
	}
	if unary, ok := ast.Unparen(expression).(*ast.UnaryExpr); ok && unary.Op == token.NOT {
		if result := e.condition(unary.X, state); result == isZero || result == notZero {
			return opposite(result)
		}
	}
	if binary, ok := ast.Unparen(expression).(*ast.BinaryExpr); ok && (binary.Op == token.EQL || binary.Op == token.NEQ) {
		for _, pair := range [][2]ast.Expr{{binary.X, binary.Y}, {binary.Y, binary.X}} {
			if e.builder.pass.TypesInfo.Types[pair[1]].IsNil() {
				if result := e.expression(pair[0], state); result == isZero || result == notZero {
					if binary.Op == token.EQL {
						result = opposite(result)
					}
					return result
				}
			}
		}
	}
	return e.expression(expression, state)
}

func (e *nilnessEvaluator) refine(expression ast.Expr, value truth, state nilnessEnv) {
	if variable, ok := expressionObject(e.builder.pass.TypesInfo, expression).(*types.Var); ok {
		if basic, ok := variable.Type().Underlying().(*types.Basic); ok && basic.Info()&types.IsBoolean != 0 &&
			!variable.IsField() && (variable.Pkg() == nil || variable.Parent() != variable.Pkg().Scope()) {
			state[variable] = value
		}
	}
	if unary, ok := ast.Unparen(expression).(*ast.UnaryExpr); ok && unary.Op == token.NOT {
		e.refine(unary.X, opposite(value), state)
	}
	if binary, ok := ast.Unparen(expression).(*ast.BinaryExpr); ok && (binary.Op == token.EQL || binary.Op == token.NEQ) {
		for _, pair := range [][2]ast.Expr{{binary.X, binary.Y}, {binary.Y, binary.X}} {
			if e.builder.pass.TypesInfo.Types[pair[1]].IsNil() {
				identifier, direct := ast.Unparen(pair[0]).(*ast.Ident)
				if !direct {
					continue
				}
				variable, ok := expressionObject(e.builder.pass.TypesInfo, identifier).(*types.Var)
				if ok && (variable.Pkg() == nil || variable.Parent() != variable.Pkg().Scope()) {
					if binary.Op == token.EQL {
						value = opposite(value)
					}
					state[variable] = value
				}
			}
		}
	}
}
