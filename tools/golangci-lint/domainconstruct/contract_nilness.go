package domainconstruct

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
)

type nilnessEnv map[*types.Var]truth

type nilnessEvaluator struct {
	builder   *contractBuilder
	signature *types.Signature
	result    int
	returns   []truth
	complete  bool
}

func (e *nilnessEvaluator) function(body *ast.BlockStmt, initial nilnessEnv) truth {
	e.returns, e.complete = nil, true
	environment := maps.Clone(initial)
	if environment == nil {
		environment = make(nilnessEnv)
	}
	for index := range e.signature.Results().Len() {
		environment[e.signature.Results().At(index)] = isZero
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
			result = e.call(call, e.result, state)
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
					value = e.call(call, index, state)
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
	if target != nil && actual != nil {
		if _, boxed := target.Underlying().(*types.Interface); boxed &&
			actual != types.Typ[types.UntypedNil] {
			if _, alreadyInterface := actual.Underlying().(*types.Interface); !alreadyInterface {
				return notZero
			}
		}
	}
	return e.expression(expression, state)
}

func (e *nilnessEvaluator) expression(expression ast.Expr, state nilnessEnv) truth {
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
				actual := e.converted(argument, types.Universe.Lookup("error").Type(), state)
				if actual == notZero {
					return notZero
				}
				if actual == unknown {
					joined = unknown
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
			if rule.Parameter == -1 {
				return notZero
			}
			if argument := callParameter(e.builder.pass.TypesInfo, call, rule.Parameter); argument != nil {
				value := e.expression(argument, state)
				if value == notZero || rule.Exact && value == isZero {
					return value
				}
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
		if result := e.condition(unary.X, state); result != unknown {
			return opposite(result)
		}
	}
	if binary, ok := ast.Unparen(expression).(*ast.BinaryExpr); ok && (binary.Op == token.EQL || binary.Op == token.NEQ) {
		for _, pair := range [][2]ast.Expr{{binary.X, binary.Y}, {binary.Y, binary.X}} {
			if e.builder.pass.TypesInfo.Types[pair[1]].IsNil() {
				if result := e.expression(pair[0], state); result != unknown {
					if binary.Op == token.EQL {
						result = opposite(result)
					}
					return result
				}
			}
		}
	}
	return unknown
}

func (e *nilnessEvaluator) refine(expression ast.Expr, value truth, state nilnessEnv) {
	if unary, ok := ast.Unparen(expression).(*ast.UnaryExpr); ok && unary.Op == token.NOT {
		e.refine(unary.X, opposite(value), state)
	}
	if binary, ok := ast.Unparen(expression).(*ast.BinaryExpr); ok && (binary.Op == token.EQL || binary.Op == token.NEQ) {
		for _, pair := range [][2]ast.Expr{{binary.X, binary.Y}, {binary.Y, binary.X}} {
			if e.builder.pass.TypesInfo.Types[pair[1]].IsNil() {
				if variable, ok := expressionObject(e.builder.pass.TypesInfo, pair[0]).(*types.Var); ok {
					if binary.Op == token.EQL {
						value = opposite(value)
					}
					state[variable] = value
				}
			}
		}
	}
}
