package domainconstruct

import (
	"cmp"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"reflect"
	"slices"
)

// Interface results retain the concrete types visible in their bodies. A
// caller that ignores an error must not lose the failure-payload obligation
// just because the producer boxed its value into an interface.
type payloadRule struct {
	Result int
	Types  []payloadType
}

type payloadKind uint8

const (
	maxPayloadAlternatives = 16
	maxPayloadDepth        = 8
)

const (
	payloadUnknown payloadKind = iota
	payloadNamed
	payloadPointer
	payloadCollection
	payloadStruct
)

type payloadType struct {
	Kind         payloadKind
	Package      string
	Name         string
	Fields       []payloadField
	SuccessOnly  bool
	JSONElements bool
}

type payloadField struct {
	Name string
	Type payloadType
}

type payloadBinding struct {
	call   *ast.CallExpr
	result int
}

func (b *contractBuilder) inferPayloads(object *types.Func, declaration *ast.FuncDecl) []payloadRule {
	signature := object.Type().(*types.Signature)
	presence := b.set.hasPresence(object)
	hasError := false
	namedStatus := false
	var rules []payloadRule
	for index := range signature.Results().Len() {
		result := signature.Results().At(index)
		status := isErrorType(result.Type())
		hasError = hasError || status
		if status || presence && index == 1 {
			namedStatus = namedStatus || result.Name() != ""
		} else if _, boxed := result.Type().Underlying().(*types.Interface); boxed {
			rules = append(rules, payloadRule{Result: index})
		}
	}
	if !hasError && !presence || len(rules) == 0 {
		return nil
	}
	environment := make(map[*types.Var][]payloadType)
	direct := make(map[ast.Node]bool)
	for _, statement := range declaration.Body.List {
		switch statement := statement.(type) {
		case *ast.AssignStmt:
			direct[statement] = true
		case *ast.DeclStmt:
			if group, ok := statement.Decl.(*ast.GenDecl); ok && group.Tok == token.VAR {
				for _, spec := range group.Specs {
					direct[spec] = true
				}
			}
		}
	}
	// A jump can bypass a replacement. A defer can change a named status
	// after evaluating the expressions in a return statement.
	var maySkipAssignment, mayChangeStatus, hasClosure bool
	unstable := make(map[*types.Var]bool)
	ast.PreorderStack(declaration.Body, nil, func(node ast.Node, ancestors []ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			hasClosure = true
			return false
		case *ast.BranchStmt:
			maySkipAssignment = maySkipAssignment || node.Tok == token.GOTO
		case *ast.DeferStmt:
			mayChangeStatus = namedStatus
		case *ast.UnaryExpr:
			if node.Op == token.AND {
				if variable, ok := expressionObject(b.pass.TypesInfo, node.X).(*types.Var); ok {
					unstable[variable] = true
				}
			}
		case *ast.SelectorExpr:
			if variable := implicitlyAddressedVariable(b.pass.TypesInfo, node); variable != nil {
				unstable[variable] = true
			}
		case *ast.AssignStmt:
			// A return can see a later assignment from the previous iteration.
			if slices.ContainsFunc(ancestors, func(ancestor ast.Node) bool {
				switch ancestor.(type) {
				case *ast.ForStmt, *ast.RangeStmt:
					return true
				}
				return false
			}) {
				for _, target := range node.Lhs {
					if variable, ok := expressionObject(b.pass.TypesInfo, target).(*types.Var); ok {
						unstable[variable] = true
					}
				}
			}
		case *ast.RangeStmt:
			if node.Tok == token.ASSIGN {
				for _, binding := range []ast.Expr{node.Key, node.Value} {
					if variable, ok := expressionObject(b.pass.TypesInfo, binding).(*types.Var); ok {
						unstable[variable] = true
					}
				}
			}
		}
		return true
	})
	bindings := make(map[*types.Var]payloadBinding)
	bindTuple := func(variable *types.Var, statement ast.Node, values []ast.Expr, result, count int) {
		delete(bindings, variable)
		if variable == nil || unstable[variable] || maySkipAssignment || hasClosure || !direct[statement] ||
			variable.Parent() == b.pass.Pkg.Scope() || len(values) != 1 || count < 2 {
			return
		}
		if call, ok := ast.Unparen(values[0]).(*ast.CallExpr); ok {
			bindings[variable] = payloadBinding{call, result}
		}
	}
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ValueSpec:
			for index, name := range node.Names {
				variable, _ := b.pass.TypesInfo.Defs[name].(*types.Var)
				bindTuple(variable, node, node.Values, index, len(node.Names))
				if len(node.Values) == len(node.Names) {
					environment[variable] = b.payloadExpression(node.Values[index], 0, environment)
				} else if len(node.Values) == 1 {
					environment[variable] = b.payloadExpression(node.Values[0], index, environment)
				}
			}
		case *ast.AssignStmt:
			type assignment struct {
				variable *types.Var
				values   []payloadType
			}
			var updates []assignment
			for index, target := range node.Lhs {
				if identifier, direct := ast.Unparen(target).(*ast.Ident); direct && (node.Tok == token.ASSIGN || node.Tok == token.DEFINE) {
					variable, _ := b.pass.TypesInfo.ObjectOf(identifier).(*types.Var)
					bindTuple(variable, node, node.Rhs, index, len(node.Lhs))
					var values []payloadType
					if len(node.Rhs) == len(node.Lhs) {
						values = b.payloadExpression(node.Rhs[index], 0, environment)
					} else if len(node.Rhs) == 1 {
						values = b.payloadExpression(node.Rhs[0], index, environment)
					}
					updates = append(updates, assignment{variable, values})
				}
			}
			for _, update := range updates {
				if direct[node] && !maySkipAssignment {
					environment[update.variable] = update.values
				} else {
					environment[update.variable] = mergePayloads(slices.Clone(environment[update.variable]), update.values)
				}
			}
		case *ast.RangeStmt:
			if node.Tok != token.ASSIGN {
				break
			}
			key, value := rangeTypes(b.pass.TypesInfo.TypeOf(node.X))
			for _, binding := range []struct {
				target ast.Expr
				source types.Type
			}{{node.Key, key}, {node.Value, value}} {
				identifier, ok := ast.Unparen(binding.target).(*ast.Ident)
				if !ok || identifier.Name == "_" {
					continue
				}
				variable, _ := b.pass.TypesInfo.ObjectOf(identifier).(*types.Var)
				if variable == nil || !interfaceValueType(variable.Type()) {
					continue
				}
				delete(bindings, variable)
				// The loop may be empty, so keep the incoming payloads.
				environment[variable] = mergePayloads(slices.Clone(environment[variable]), concretePayload(binding.source))
			}
		case *ast.ReturnStmt:
			status := unknown
			if !mayChangeStatus {
				status = b.payloadReturnStatus(signature, presence, node)
			}
			for index, rule := range rules {
				var values []payloadType
				if len(node.Results) == signature.Results().Len() {
					values = b.payloadExpression(node.Results[rule.Result], 0, environment)
				} else if len(node.Results) == 1 {
					values = b.payloadExpression(node.Results[0], rule.Result, environment)
				} else if len(node.Results) == 0 {
					values = environment[signature.Results().At(rule.Result)]
				}
				forwarded := status == unknown && !mayChangeStatus &&
					b.payloadStatusForwarded(signature, presence, node, rule.Result, bindings)
				for _, value := range values {
					if !forwarded {
						value.SuccessOnly = status == isZero
					}
					rule.Types = mergePayloads(rule.Types, []payloadType{value})
				}
				rules[index] = rule
			}
		}
		return true
	})
	var result []payloadRule
	for _, rule := range rules {
		if len(rule.Types) != 0 {
			slices.SortFunc(rule.Types, comparePayloads)
			result = append(result, rule)
		}
	}
	return result
}

// A success-only payload keeps that qualification only while the wrapper
// returns its original status. The local form requires matching bindings
// from a single call whose results have not been reassigned or exposed.
func (b *contractBuilder) payloadStatusForwarded(signature *types.Signature, presence bool, statement *ast.ReturnStmt, result int, bindings map[*types.Var]payloadBinding) bool {
	if len(statement.Results) == 0 {
		return false
	}
	bindingAt := func(index int) payloadBinding {
		if len(statement.Results) == 1 {
			call, _ := ast.Unparen(statement.Results[0]).(*ast.CallExpr)
			return payloadBinding{call, index}
		}
		variable, _ := expressionObject(b.pass.TypesInfo, statement.Results[index]).(*types.Var)
		return bindings[variable]
	}
	payload := bindingAt(result)
	if payload.call == nil {
		return false
	}
	tuple, ok := b.pass.TypesInfo.TypeOf(payload.call).(*types.Tuple)
	if !ok {
		return false
	}
	source, _ := expressionObject(b.pass.TypesInfo, payload.call.Fun).(*types.Func)
	results := signature.Results()
	for index := range results.Len() {
		target := results.At(index)
		presenceStatus := presence && index == 1
		if !presenceStatus && !isErrorType(target.Type()) {
			continue
		}
		status := bindingAt(index)
		if status.call != payload.call || !types.Identical(tuple.At(status.result).Type(), target.Type()) {
			return false
		}
		if presenceStatus && (status.result != 1 || !b.set.hasPresence(source)) {
			return false
		}
	}
	return true
}

// An explicit failure can make a previously successful boxed result inert.
// Forwarded statuses keep the producer's existing success and failure facts.
func (b *contractBuilder) payloadReturnStatus(signature *types.Signature, presence bool, statement *ast.ReturnStmt) truth {
	if len(statement.Results) != signature.Results().Len() {
		return unknown
	}
	status := isZero
	work := workBudget(maxInferenceWork)
	nilness := nilnessEvaluator{builder: b, complete: true, work: &work}
	for index, expression := range statement.Results {
		target := signature.Results().At(index).Type()
		if isErrorType(target) {
			switch nilness.converted(expression, target, nil) {
			case notZero:
				return notZero
			case unknown:
				status = unknown
			}
		}
		if index == 1 && presence {
			if value := b.pass.TypesInfo.Types[expression].Value; value != nil && value.Kind() == constant.Bool {
				if !constant.BoolVal(value) {
					return notZero
				}
			} else {
				status = unknown
			}
		}
	}
	return status
}

func (b *contractBuilder) payloadExpression(expression ast.Expr, result int, environment map[*types.Var][]payloadType) []payloadType {
	expression = ast.Unparen(expression)
	t := b.pass.TypesInfo.TypeOf(expression)
	if tuple, ok := t.(*types.Tuple); ok {
		t = tuple.At(result).Type()
	}
	if t == nil {
		return nil
	}
	if _, pointer := t.Underlying().(*types.Pointer); pointer {
		if conversion, ok := expression.(*ast.CallExpr); ok && len(conversion.Args) == 1 &&
			b.pass.TypesInfo.Types[conversion.Fun].IsType() && b.pass.TypesInfo.Types[conversion.Args[0]].IsNil() {
			return nil // A boxed nil pointer does not contain a pointee.
		}
	}
	if described := concretePayload(t); len(described) != 0 {
		return described
	}
	if variable, ok := expressionObject(b.pass.TypesInfo, expression).(*types.Var); ok {
		return environment[variable]
	}
	if call, ok := expression.(*ast.CallExpr); ok {
		if b.pass.TypesInfo.Types[call.Fun].IsType() && len(call.Args) == 1 {
			if _, boxed := t.Underlying().(*types.Interface); boxed {
				return b.payloadExpression(call.Args[0], 0, environment)
			}
			return nil
		}
		if function, ok := expressionObject(b.pass.TypesInfo, call.Fun).(*types.Func); ok {
			if fact := b.function(function); fact != nil {
				for _, rule := range fact.Payloads {
					if rule.Result == result {
						return rule.Types
					}
				}
			}
		}
	}
	return nil
}

func concretePayload(t types.Type) []payloadType {
	if t == nil {
		return nil
	}
	if _, boxed := t.Underlying().(*types.Interface); boxed {
		return nil
	}
	work := workBudget(maxInferenceWork)
	if described := describePayload(t, make(map[types.Type]bool), 0, false, &work); described.Kind != payloadUnknown {
		return []payloadType{described}
	}
	return nil
}

func describePayload(t types.Type, seen map[types.Type]bool, depth int, addressable bool, work *workBudget) payloadType {
	if !work.take() || seen[t] || depth > maxPayloadDepth || optionalPayload(t) != nil {
		return payloadType{}
	}
	seen[t] = true
	defer delete(seen, t)
	t = types.Unalias(t)
	if named, ok := t.(*types.Named); ok {
		object := named.Obj()
		if object.Pkg() != nil && object.Parent() == object.Pkg().Scope() {
			underlying := describePayload(named.Underlying(), seen, depth+1, addressable, work)
			return payloadType{
				Kind: payloadNamed, Package: object.Pkg().Path(), Name: object.Name(),
				Fields: underlying.Fields, JSONElements: len(underlying.Fields) != 0 && usesDefaultJSONElements(named, addressable),
			}
		}
	}
	switch t := t.Underlying().(type) {
	case *types.Pointer:
		return payloadType{Kind: payloadPointer, Fields: []payloadField{{Type: describePayload(t.Elem(), seen, depth+1, true, work)}}}
	case *types.Slice:
		return payloadType{Kind: payloadCollection, Fields: []payloadField{{Type: describePayload(t.Elem(), seen, depth+1, true, work)}}}
	case *types.Array:
		if t.Len() != 0 {
			return payloadType{Kind: payloadCollection, Fields: []payloadField{{Type: describePayload(t.Elem(), seen, depth+1, addressable, work)}}}
		}
	case *types.Map:
		return payloadType{Kind: payloadCollection, Fields: []payloadField{
			{Type: describePayload(t.Key(), seen, depth+1, false, work)},
			{Type: describePayload(t.Elem(), seen, depth+1, false, work)},
		}}
	case *types.Struct:
		result := payloadType{Kind: payloadStruct}
		if depth == maxPayloadDepth {
			return result
		}
		for field := range t.Fields() {
			if work.exhausted() {
				break
			}
			result.Fields = append(result.Fields, payloadField{field.Name(), describePayload(field.Type(), seen, depth+1, addressable, work)})
		}
		return result
	}
	return payloadType{}
}

func usesDefaultJSONElements(t types.Type, addressable bool) bool {
	switch t.Underlying().(type) {
	case *types.Slice, *types.Array, *types.Map:
	default:
		return false
	}
	use := jsonFields
	if addressable {
		use = jsonAddressableFields
	}
	return jsonUseForType(t, use).json()
}

func mergePayloads(existing, added []payloadType) []payloadType {
	for _, value := range added {
		found := false
		for _, previous := range existing {
			if reflect.DeepEqual(previous, value) {
				found = true
				break
			}
		}
		if !found {
			if len(existing) < maxPayloadAlternatives {
				existing = append(existing, value)
			} else if !value.SuccessOnly {
				if replace := slices.IndexFunc(existing, func(previous payloadType) bool { return previous.SuccessOnly }); replace >= 0 {
					existing[replace] = value
				}
			}
		}
	}
	return existing
}

func comparePayloads(first, second payloadType) int {
	if order := cmp.Or(cmp.Compare(first.Kind, second.Kind), cmp.Compare(first.Package, second.Package), cmp.Compare(first.Name, second.Name)); order != 0 {
		return order
	}
	if first.SuccessOnly != second.SuccessOnly {
		if first.SuccessOnly {
			return 1
		}
		return -1
	}
	return slices.CompareFunc(first.Fields, second.Fields, func(a, b payloadField) int {
		if order := cmp.Compare(a.Name, b.Name); order != 0 {
			return order
		}
		return comparePayloads(a.Type, b.Type)
	})
}
