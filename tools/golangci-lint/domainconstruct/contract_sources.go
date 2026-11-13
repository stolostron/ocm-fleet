package domainconstruct

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"reflect"
	"slices"
)

// Source summaries preserve the origin of generic results until the caller
// supplies concrete type arguments. They describe construction, not validation.
type sourceKind uint8

const (
	sourceUnknown sourceKind = iota
	sourceZero
	sourceParameter
	sourcePointer
	sourceSlice
	sourceFields
	sourceChoice
	sourceConversion
)

type resultSource struct {
	Result int
	Value  valueSource
}

type valueSource struct {
	Kind      sourceKind
	Parameter int // -1 is the receiver.
	Path      []int
	Values    []valueSource
	Length    sourceLength
}

type sourceLength struct {
	Constant  int64 // -1 is unknown.
	Parameter int   // -1 means there is no parameter.
	FromLen   bool
}

type sourceState struct {
	values map[*types.Var]valueSource
	errors nilnessEnv
}

func (state sourceState) clone() sourceState {
	return sourceState{maps.Clone(state.values), maps.Clone(state.errors)}
}

type sourceEvaluator struct {
	builder   *contractBuilder
	object    *types.Func
	signature *types.Signature
	returns   [][]valueSource
	complete  bool
}

func (b *contractBuilder) inferSources(object *types.Func, declaration *ast.FuncDecl) []resultSource {
	signature := object.Type().(*types.Signature)
	if signature.TypeParams().Len() == 0 && signature.RecvTypeParams().Len() == 0 {
		return nil
	}
	e := sourceEvaluator{builder: b, object: object, signature: signature, complete: true}
	state := sourceState{values: make(map[*types.Var]valueSource), errors: make(nilnessEnv)}
	for index := range signature.Params().Len() {
		state.values[signature.Params().At(index)] = valueSource{Kind: sourceParameter, Parameter: index}
	}
	if receiver := signature.Recv(); receiver != nil {
		state.values[receiver] = valueSource{Kind: sourceParameter, Parameter: -1}
	}
	for index := range signature.Results().Len() {
		state.values[signature.Results().At(index)] = valueSource{Kind: sourceZero}
		state.errors[signature.Results().At(index)] = isZero
	}
	remaining := e.block(declaration.Body.List, []sourceState{state})
	if !e.complete || len(remaining) != 0 || len(e.returns) == 0 {
		return nil
	}
	var results []resultSource
	for index := range signature.Results().Len() {
		if isErrorType(signature.Results().At(index).Type()) {
			continue
		}
		var alternatives []valueSource
		for _, returned := range e.returns {
			alternatives = append(alternatives, returned[index])
		}
		value := combineSources(alternatives)
		if value.Kind != sourceUnknown {
			results = append(results, resultSource{index, value})
		}
	}
	return results
}

func (e *sourceEvaluator) block(statements []ast.Stmt, states []sourceState) []sourceState {
	for _, statement := range statements {
		var next []sourceState
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

func (e *sourceEvaluator) statement(statement ast.Stmt, state sourceState) []sourceState {
	// Captures and address escapes can modify an apparent source indirectly.
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
	nilness := nilnessEvaluator{builder: e.builder, complete: true}
	switch statement := statement.(type) {
	case *ast.ReturnStmt:
		values := make([]valueSource, e.signature.Results().Len())
		for index := range values {
			var expression ast.Expr
			if len(statement.Results) == 0 {
				values[index] = state.values[e.signature.Results().At(index)]
			} else if len(statement.Results) == len(values) {
				expression = statement.Results[index]
				values[index] = e.expression(expression, state)
			} else if call, ok := ast.Unparen(statement.Results[0]).(*ast.CallExpr); ok {
				values[index] = e.call(call, index, state)
			}
			// Only successful returns contribute domain values. Known failure
			// payloads remain inert even after instantiation.
			if isErrorType(e.signature.Results().At(index).Type()) {
				actual := state.errors[e.signature.Results().At(index)]
				if expression != nil {
					actual = nilness.converted(expression, e.signature.Results().At(index).Type(), state.errors)
				}
				if actual == notZero {
					return nil
				}
			}
		}
		e.returns = append(e.returns, values)
		return nil
	case *ast.BlockStmt:
		return e.block(statement.List, []sourceState{state})
	case *ast.IfStmt:
		if statement.Init != nil {
			if states := e.statement(statement.Init, state); len(states) != 1 {
				e.complete = false
				return nil
			}
		}
		condition := nilness.condition(statement.Cond, state.errors)
		var next []sourceState
		if condition != isZero {
			branch := state.clone()
			nilness.refine(statement.Cond, notZero, branch.errors)
			next = append(next, e.block(statement.Body.List, []sourceState{branch})...)
		}
		if condition != notZero {
			branch := state.clone()
			nilness.refine(statement.Cond, isZero, branch.errors)
			if statement.Else != nil {
				next = append(next, e.statement(statement.Else, branch)...)
			} else {
				next = append(next, branch)
			}
		}
		return next
	case *ast.AssignStmt:
		values := make([]valueSource, len(statement.Lhs))
		for index := range values {
			if len(statement.Rhs) == len(values) {
				values[index] = e.expression(statement.Rhs[index], state)
			} else if len(statement.Rhs) == 1 {
				if call, ok := ast.Unparen(statement.Rhs[0]).(*ast.CallExpr); ok {
					values[index] = e.call(call, index, state)
				}
			}
		}
		for index, target := range statement.Lhs {
			identifier, ok := ast.Unparen(target).(*ast.Ident)
			variable, variableOK := expressionObject(e.builder.pass.TypesInfo, target).(*types.Var)
			if !ok || !variableOK || variable.Pkg() != nil && variable.Parent() == variable.Pkg().Scope() {
				if !ok || identifier.Name != "_" {
					e.complete = false
				}
				continue
			}
			state.values[variable] = values[index]
		}
		nilness.statement(statement, state.errors)
		e.complete = e.complete && nilness.complete
	case *ast.DeclStmt:
		declaration, ok := statement.Decl.(*ast.GenDecl)
		if !ok || declaration.Tok != token.VAR {
			e.complete = false
			break
		}
		for _, spec := range declaration.Specs {
			spec := spec.(*ast.ValueSpec)
			for index, name := range spec.Names {
				value := valueSource{Kind: sourceZero}
				if len(spec.Values) == len(spec.Names) {
					value = e.expression(spec.Values[index], state)
				} else if len(spec.Values) != 0 {
					e.complete = false
				}
				state.values[e.builder.pass.TypesInfo.Defs[name].(*types.Var)] = value
			}
		}
		nilness.statement(statement, state.errors)
		e.complete = e.complete && nilness.complete
	case *ast.ExprStmt:
		if call, ok := statement.X.(*ast.CallExpr); ok {
			if builtin, ok := expressionObject(e.builder.pass.TypesInfo, call.Fun).(*types.Builtin); ok && builtin.Name() == "panic" {
				return nil
			}
		}
	case *ast.EmptyStmt:
	default:
		e.complete = false
	}
	return []sourceState{state}
}

func (e *sourceEvaluator) expression(expression ast.Expr, state sourceState) valueSource {
	expression = ast.Unparen(expression)
	if value := e.builder.pass.TypesInfo.Types[expression]; value.IsNil() || zeroLiteral(value.Value) {
		return valueSource{Kind: sourceZero}
	}
	switch expression := expression.(type) {
	case *ast.Ident:
		if variable, ok := expressionObject(e.builder.pass.TypesInfo, expression).(*types.Var); ok {
			return state.values[variable]
		}
	case *ast.SelectorExpr:
		if selection := e.builder.pass.TypesInfo.Selections[expression]; selection != nil && selection.Kind() == types.FieldVal {
			return projectSource(e.expression(expression.X, state), selection.Index())
		}
	case *ast.StarExpr:
		pointer := e.expression(expression.X, state)
		if pointer.Kind == sourcePointer && len(pointer.Values) == 1 {
			return pointer.Values[0]
		}
	case *ast.UnaryExpr:
		if expression.Op == token.AND {
			return valueSource{Kind: sourcePointer, Values: []valueSource{e.expression(expression.X, state)}}
		}
	case *ast.CompositeLit:
		if structure, ok := e.builder.pass.TypesInfo.TypeOf(expression).Underlying().(*types.Struct); ok {
			fields := make([]valueSource, structure.NumFields())
			for index := range fields {
				fields[index].Kind = sourceZero
			}
			for index, element := range expression.Elts {
				if keyed, ok := element.(*ast.KeyValueExpr); ok {
					field := expressionObject(e.builder.pass.TypesInfo, keyed.Key)
					for position := range structure.NumFields() {
						if structure.Field(position) == field {
							fields[position] = e.expression(keyed.Value, state)
						}
					}
				} else if index < len(fields) {
					fields[index] = e.expression(element, state)
				}
			}
			return valueSource{Kind: sourceFields, Values: fields}
		}
		if len(expression.Elts) == 0 {
			return valueSource{Kind: sourceZero}
		}
	case *ast.CallExpr:
		return e.call(expression, 0, state)
	}
	return valueSource{}
}

func (e *sourceEvaluator) call(call *ast.CallExpr, result int, state sourceState) valueSource {
	if e.builder.pass.TypesInfo.Types[call.Fun].IsType() && len(call.Args) == 1 {
		if types.Identical(e.builder.pass.TypesInfo.TypeOf(call), e.builder.pass.TypesInfo.TypeOf(call.Args[0])) {
			return e.expression(call.Args[0], state)
		}
		return valueSource{Kind: sourceConversion, Values: []valueSource{e.expression(call.Args[0], state)}}
	}
	if builtin, ok := expressionObject(e.builder.pass.TypesInfo, call.Fun).(*types.Builtin); ok {
		switch builtin.Name() {
		case "new":
			return valueSource{Kind: sourcePointer, Values: []valueSource{{Kind: sourceZero}}}
		case "make":
			if _, slice := e.builder.pass.TypesInfo.TypeOf(call).Underlying().(*types.Slice); slice && len(call.Args) >= 2 {
				length := sourceLength{Constant: -1, Parameter: -1}
				if number := e.builder.pass.TypesInfo.Types[call.Args[1]].Value; number != nil && number.Kind() == constant.Int {
					length.Constant, _ = constant.Int64Val(number)
				} else {
					expression := call.Args[1]
					if size, ok := ast.Unparen(expression).(*ast.CallExpr); ok && len(size.Args) == 1 {
						if builtin, ok := expressionObject(e.builder.pass.TypesInfo, size.Fun).(*types.Builtin); ok && builtin.Name() == "len" {
							expression, length.FromLen = size.Args[0], true
						}
					}
					if source := e.expression(expression, state); source.Kind == sourceParameter && len(source.Path) == 0 {
						length.Parameter = source.Parameter
					}
				}
				return valueSource{Kind: sourceSlice, Length: length}
			}
		}
	}
	if function, ok := expressionObject(e.builder.pass.TypesInfo, call.Fun).(*types.Func); ok && function.Origin() != e.object {
		if fact := e.builder.function(function); fact != nil {
			for _, source := range fact.Sources {
				if source.Result == result {
					return substituteSource(source.Value, func(parameter int) valueSource {
						if argument := callParameter(e.builder.pass.TypesInfo, call, parameter); argument != nil {
							return e.expression(argument, state)
						}
						return valueSource{}
					}, 0)
				}
			}
		}
	}
	return valueSource{}
}

func projectSource(source valueSource, path []int) valueSource {
	for _, field := range path {
		switch source.Kind {
		case sourceZero:
			return source
		case sourceParameter:
			source.Path = append(slices.Clone(source.Path), field)
		case sourceFields:
			if field >= len(source.Values) {
				return valueSource{}
			}
			source = source.Values[field]
		default:
			return valueSource{}
		}
	}
	return source
}

func substituteSource(source valueSource, parameter func(int) valueSource, depth int) valueSource {
	if depth > 8 {
		return valueSource{}
	}
	if source.Kind == sourceParameter {
		return projectSource(parameter(source.Parameter), source.Path)
	}
	source.Values = slices.Clone(source.Values)
	for index, child := range source.Values {
		source.Values[index] = substituteSource(child, parameter, depth+1)
	}
	if source.Kind == sourceSlice && source.Length.Parameter >= 0 {
		argument := parameter(source.Length.Parameter)
		if argument.Kind == sourceParameter && len(argument.Path) == 0 {
			source.Length.Parameter = argument.Parameter
		} else {
			source.Length.Parameter = -1
		}
	}
	if source.Kind == sourceChoice {
		return combineSources(source.Values)
	}
	return source
}

func combineSources(values []valueSource) valueSource {
	var alternatives []valueSource
	var add func(valueSource)
	add = func(value valueSource) {
		if value.Kind == sourceChoice {
			for _, child := range value.Values {
				add(child)
			}
			return
		}
		for _, existing := range alternatives {
			if reflect.DeepEqual(existing, value) {
				return
			}
		}
		alternatives = append(alternatives, value)
	}
	for _, value := range values {
		add(value)
	}
	if len(alternatives) == 1 {
		return alternatives[0]
	}
	return valueSource{Kind: sourceChoice, Values: alternatives}
}

func zeroLiteral(value constant.Value) bool {
	if value == nil {
		return false
	}
	switch value.Kind() {
	case constant.String:
		return constant.StringVal(value) == ""
	case constant.Bool:
		return !constant.BoolVal(value)
	case constant.Int, constant.Float:
		return constant.Sign(value) == 0
	case constant.Complex:
		return constant.Sign(constant.Real(value)) == 0 && constant.Sign(constant.Imag(value)) == 0
	}
	return false
}
