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

const maxSourceDepth = 8

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
	Kind           sourceKind
	Parameter      int // -1 is the receiver.
	Path           []int
	Values         []valueSource
	Length         sourceLength
	ZeroKind       constant.Kind
	ConversionType int // One-based function type parameter of the target.
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
	returns   []sourceReturn
	complete  bool
	work      workBudget
}

type sourceReturn struct {
	values     []valueSource
	successful bool
}

func (b *contractBuilder) inferSources(object *types.Func, declaration *ast.FuncDecl) (successful, failed []resultSource) {
	signature := object.Type().(*types.Signature)
	if signature.TypeParams().Len() == 0 && signature.RecvTypeParams().Len() == 0 {
		return nil, nil
	}
	e := sourceEvaluator{builder: b, object: object, signature: signature, complete: true, work: maxInferenceWork}
	state := sourceState{values: make(map[*types.Var]valueSource), errors: make(nilnessEnv)}
	for index := range signature.Params().Len() {
		state.values[signature.Params().At(index)] = valueSource{Kind: sourceParameter, Parameter: index}
	}
	if receiver := signature.Recv(); receiver != nil {
		state.values[receiver] = valueSource{Kind: sourceParameter, Parameter: -1}
	}
	for result := range signature.Results().Variables() {
		state.values[result] = implicitZeroSource(result.Type())
		state.errors[result] = isZero
	}
	remaining := e.block(declaration.Body.List, []sourceState{state})
	if !e.complete || len(remaining) != 0 || len(e.returns) == 0 {
		return nil, nil
	}
	for index := range signature.Results().Len() {
		if isErrorType(signature.Results().At(index).Type()) {
			continue
		}
		var accepted, rejected []valueSource
		for _, returned := range e.returns {
			// Count the expanded shape even when source subtrees share slices.
			// Fact comparison and export consume the expanded representation.
			value := returned.values[index]
			if !sourceFits(value, 0, &e.work) {
				return nil, nil
			}
			if returned.successful {
				accepted = append(accepted, value)
			} else if value.hasConversion() {
				rejected = append(rejected, value)
			}
		}
		var complete bool
		successful, complete = recordResultSource(successful, index, accepted, &e.work)
		if !complete {
			return nil, nil
		}
		failed, complete = recordResultSource(failed, index, rejected, &e.work)
		if !complete {
			return nil, nil
		}
	}
	return successful, failed
}

func recordResultSource(results []resultSource, index int, alternatives []valueSource, work *workBudget) ([]resultSource, bool) {
	if len(alternatives) == 0 {
		return results, true
	}
	value := combineSources(alternatives)
	if value.Kind == sourceChoice && !sourceFits(value, 0, work) {
		return nil, false
	}
	if value.Kind != sourceUnknown {
		results = append(results, resultSource{index, value})
	}
	return results, true
}

func (source valueSource) hasConversion() bool {
	if source.Kind == sourceConversion {
		return true
	}
	for _, child := range source.Values {
		if child.hasConversion() {
			return true
		}
	}
	return false
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
		if selector, ok := node.(*ast.SelectorExpr); ok && implicitlyAddressedVariable(e.builder.pass.TypesInfo, selector) != nil {
			e.complete = false
		}
		return true
	})
	if !e.complete {
		return nil
	}
	nilness := nilnessEvaluator{builder: e.builder, complete: true, work: &e.work}
	switch statement := statement.(type) {
	case *ast.ReturnStmt:
		e.recordReturn(statement, state, &nilness)
		e.complete = e.complete && nilness.complete
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
			if statement.Tok == token.DEFINE || statement.Tok == token.ASSIGN {
				state.values[variable] = values[index]
			} else {
				delete(state.values, variable)
			}
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
				variable := e.builder.pass.TypesInfo.Defs[name].(*types.Var)
				value := implicitZeroSource(variable.Type())
				if len(spec.Values) == len(spec.Names) {
					value = e.expression(spec.Values[index], state)
				} else if len(spec.Values) != 0 {
					e.complete = false
				}
				state.values[variable] = value
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

func (e *sourceEvaluator) recordReturn(statement *ast.ReturnStmt, state sourceState, nilness *nilnessEvaluator) {
	results := e.signature.Results()
	returned := sourceReturn{values: make([]valueSource, results.Len()), successful: true}
	var forwarded []valueSource
	for index := range results.Len() {
		result := results.At(index)
		presence := index == 1 && e.builder.set.hasPresence(e.object)
		status := presence || isErrorType(result.Type())
		actual := unknown
		var call *ast.CallExpr
		callResult := 0
		switch {
		case len(statement.Results) == 0:
			returned.values[index] = state.values[result]
			actual = state.errors[result]
		case len(statement.Results) == results.Len():
			expression := statement.Results[index]
			returned.values[index] = e.expression(expression, state)
			if status {
				actual = nilness.converted(expression, result.Type(), state.errors)
			} else {
				call, _ = ast.Unparen(expression).(*ast.CallExpr)
			}
		default:
			if spread, ok := ast.Unparen(statement.Results[0]).(*ast.CallExpr); ok {
				returned.values[index] = e.call(spread, index, state)
				switch {
				case presence:
					if returned.values[index].Kind == sourceZero {
						actual = isZero
					}
				case status:
					operand := e.builder.pass.TypesInfo.TypeOf(spread).(*types.Tuple).At(index).Type()
					actual = nilness.convertedResult(spread, index, operand, result.Type(), state.errors)
				default:
					call, callResult = spread, index
				}
			}
		}
		// Only present, successful results can supply initialization facts.
		// Construction checks also need conversions that ran before failure.
		if status && (presence && actual == isZero || !presence && actual == notZero) {
			returned.successful = false
		}
		if call != nil {
			if value := e.inferredSource(call, callResult, state, true); value.Kind != sourceUnknown {
				if forwarded == nil {
					forwarded = make([]valueSource, results.Len())
				}
				forwarded[index] = value
			}
		}
	}
	e.returns = append(e.returns, returned)
	if forwarded != nil {
		e.returns = append(e.returns, sourceReturn{values: forwarded})
	}
}

func (e *sourceEvaluator) expression(expression ast.Expr, state sourceState) valueSource {
	if !e.complete || !e.work.take() {
		e.complete = false
		return valueSource{}
	}
	expression = ast.Unparen(expression)
	if value := e.builder.pass.TypesInfo.Types[expression]; value.IsNil() || zeroLiteral(value.Value) {
		source := valueSource{Kind: sourceZero}
		if value.Value != nil {
			source.ZeroKind = value.Value.Kind()
		}
		return source
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
			count := workBudget(structure.NumFields())
			if count > e.work {
				e.complete = false
				return valueSource{}
			}
			e.work -= count
			fields := make([]valueSource, structure.NumFields())
			for index := range fields {
				fields[index] = implicitZeroSource(structure.Field(index).Type())
			}
			for index, element := range expression.Elts {
				if keyed, ok := element.(*ast.KeyValueExpr); ok {
					field := expressionObject(e.builder.pass.TypesInfo, keyed.Key)
					for position := range structure.NumFields() {
						if !e.work.take() {
							e.complete = false
							return valueSource{}
						}
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
		if _, array := e.builder.pass.TypesInfo.TypeOf(expression).Underlying().(*types.Array); array && len(expression.Elts) == 0 {
			return valueSource{Kind: sourceZero}
		}
	case *ast.CallExpr:
		return e.call(expression, 0, state)
	}
	return valueSource{}
}

func (e *sourceEvaluator) call(call *ast.CallExpr, result int, state sourceState) valueSource {
	if !e.complete || !e.work.take() {
		e.complete = false
		return valueSource{}
	}
	if e.builder.pass.TypesInfo.Types[call.Fun].IsType() && len(call.Args) == 1 {
		target := e.builder.pass.TypesInfo.TypeOf(call)
		if interfaceValueType(target) || types.Identical(target, e.builder.pass.TypesInfo.TypeOf(call.Args[0])) {
			return e.expression(call.Args[0], state)
		}
		return valueSource{
			Kind: sourceConversion, Values: []valueSource{e.expression(call.Args[0], state)},
			ConversionType: declaredTypeParameter(target, e.signature),
		}
	}
	if builtin, ok := expressionObject(e.builder.pass.TypesInfo, call.Fun).(*types.Builtin); ok {
		switch builtin.Name() {
		case "new":
			pointee := implicitZeroSource(e.builder.pass.TypesInfo.TypeOf(call.Args[0]))
			if !e.builder.pass.TypesInfo.Types[call.Args[0]].IsType() {
				pointee = e.expression(call.Args[0], state)
			}
			return valueSource{Kind: sourcePointer, Values: []valueSource{pointee}}
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
	return e.inferredSource(call, result, state, false)
}

func (e *sourceEvaluator) inferredSource(call *ast.CallExpr, result int, state sourceState, failed bool) valueSource {
	if function, ok := expressionObject(e.builder.pass.TypesInfo, call.Fun).(*types.Func); ok && function.Origin() != e.object {
		if fact := e.builder.function(function); fact != nil {
			sources := fact.Sources
			if failed {
				sources = fact.FailedSources
			}
			for _, source := range sources {
				if source.Result == result {
					arguments := make(map[int]valueSource)
					bound, complete := substituteSource(source.Value, func(parameter int) valueSource {
						if value, evaluated := arguments[parameter]; evaluated {
							return value
						}
						var value valueSource
						if argument := callParameter(e.builder.pass.TypesInfo, call, parameter); argument.expression != nil {
							if argument.result < 0 {
								value = e.expression(argument.expression, state)
							} else if spread, ok := ast.Unparen(argument.expression).(*ast.CallExpr); ok {
								value = e.call(spread, argument.result, state)
							}
							value = projectSource(value, argument.projection)
						}
						arguments[parameter] = value
						return value
					}, func(index int) int {
						return declaredTypeParameter(calledTypeArgument(e.builder.pass.TypesInfo, call, index), e.signature)
					}, 0, &e.work)
					e.complete = e.complete && complete
					return bound
				}
			}
		}
	}
	return valueSource{}
}

func projectSource(source valueSource, path []int) valueSource {
	const maxSourcePath = 8
	for index, field := range path {
		if source.Kind == sourcePointer && len(source.Values) == 1 {
			source = source.Values[0] // A field selection implicitly dereferences the pointer.
		}
		switch source.Kind {
		case sourceZero:
			return source
		case sourceParameter:
			if len(source.Path) == maxSourcePath {
				return valueSource{}
			}
			source.Path = append(slices.Clone(source.Path), field)
		case sourceFields:
			if field >= len(source.Values) {
				return valueSource{}
			}
			source = source.Values[field]
		case sourceChoice:
			projected := make([]valueSource, len(source.Values))
			for alternative, child := range source.Values {
				projected[alternative] = projectSource(child, path[index:])
			}
			return combineSources(projected)
		default:
			return valueSource{}
		}
	}
	return source
}

func implicitZeroSource(t types.Type) valueSource {
	source := valueSource{Kind: sourceZero}
	if basic, ok := t.Underlying().(*types.Basic); ok {
		switch info := basic.Info(); {
		case info&types.IsInteger != 0:
			source.ZeroKind = constant.Int
		case info&types.IsString != 0:
			source.ZeroKind = constant.String
		case info&types.IsBoolean != 0:
			source.ZeroKind = constant.Bool
		case info&types.IsFloat != 0:
			source.ZeroKind = constant.Float
		case info&types.IsComplex != 0:
			source.ZeroKind = constant.Complex
		}
	}
	return source
}

func substituteSource(source valueSource, parameter func(int) valueSource, typeParameter func(int) int, depth int, work *workBudget) (valueSource, bool) {
	if depth > maxSourceDepth || !work.take() {
		return valueSource{}, false
	}
	if source.Kind == sourceParameter {
		argument := projectSource(parameter(source.Parameter), source.Path)
		return argument, sourceFits(argument, depth, work)
	}
	source.Values = slices.Clone(source.Values)
	for index, child := range source.Values {
		bound, complete := substituteSource(child, parameter, typeParameter, depth+1, work)
		if !complete {
			return valueSource{}, false
		}
		source.Values[index] = bound
	}
	if source.ConversionType != 0 {
		source.ConversionType = typeParameter(source.ConversionType)
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
		return combineSources(source.Values), true
	}
	return source, true
}

func sourceFits(source valueSource, depth int, work *workBudget) bool {
	if depth > maxSourceDepth || !work.take() {
		return false
	}
	for _, child := range source.Values {
		if !sourceFits(child, depth+1, work) {
			return false
		}
	}
	return true
}

func combineSources(values []valueSource) valueSource {
	const maxAlternatives = 16
	var alternatives []valueSource
	var add func(valueSource) bool
	add = func(value valueSource) bool {
		if value.Kind == sourceChoice {
			for _, child := range value.Values {
				if !add(child) {
					return false
				}
			}
			return true
		}
		for _, existing := range alternatives {
			if reflect.DeepEqual(existing, value) {
				return true
			}
		}
		if len(alternatives) == maxAlternatives {
			return false
		}
		alternatives = append(alternatives, value)
		return true
	}
	for _, value := range values {
		if !add(value) {
			return valueSource{}
		}
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
