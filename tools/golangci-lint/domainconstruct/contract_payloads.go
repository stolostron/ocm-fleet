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

const maxPayloadAlternatives = 16

const (
	payloadUnknown payloadKind = iota
	payloadNamed
	payloadPointer
	payloadCollection
	payloadStruct
)

type payloadType struct {
	Kind    payloadKind
	Package string
	Name    string
	Fields  []payloadField
}

type payloadField struct {
	Name string
	Type payloadType
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
	directAssignments := make(map[*ast.AssignStmt]bool)
	for _, statement := range declaration.Body.List {
		if assignment, ok := statement.(*ast.AssignStmt); ok {
			directAssignments[assignment] = true
		}
	}
	// A jump can bypass a replacement. A defer can change a named status
	// after evaluating the expressions in a return statement.
	var maySkipAssignment, mayChangeStatus bool
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.BranchStmt:
			maySkipAssignment = maySkipAssignment || node.Tok == token.GOTO
		case *ast.DeferStmt:
			mayChangeStatus = namedStatus
		}
		return true
	})
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ValueSpec:
			for index, name := range node.Names {
				variable, _ := b.pass.TypesInfo.Defs[name].(*types.Var)
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
				if directAssignments[node] && !maySkipAssignment {
					environment[update.variable] = update.values
				} else {
					environment[update.variable] = mergePayloads(slices.Clone(environment[update.variable]), update.values)
				}
			}
		case *ast.ReturnStmt:
			if !mayChangeStatus && b.payloadReturnSucceeds(signature, presence, node) {
				return true
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
				rules[index].Types = mergePayloads(rule.Types, values)
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

func (b *contractBuilder) payloadReturnSucceeds(signature *types.Signature, presence bool, statement *ast.ReturnStmt) bool {
	if len(statement.Results) != signature.Results().Len() {
		return false
	}
	for index, expression := range statement.Results {
		value := b.pass.TypesInfo.Types[expression]
		if isErrorType(signature.Results().At(index).Type()) && !value.IsNil() {
			return false
		}
		if index == 1 && presence && (value.Value == nil || value.Value.Kind() != constant.Bool || !constant.BoolVal(value.Value)) {
			return false
		}
	}
	return true
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
	if _, boxed := t.Underlying().(*types.Interface); !boxed {
		work := workBudget(maxInferenceWork)
		if described := describePayload(t, make(map[types.Type]bool), 0, &work); described.Kind != payloadUnknown {
			return []payloadType{described}
		}
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

func describePayload(t types.Type, seen map[types.Type]bool, depth int, work *workBudget) payloadType {
	if seen[t] || depth > 8 || !work.take() || optionalPayload(t) != nil {
		return payloadType{}
	}
	seen[t] = true
	defer delete(seen, t)
	t = types.Unalias(t)
	if named, ok := t.(*types.Named); ok {
		object := named.Obj()
		if object.Pkg() != nil && object.Parent() == object.Pkg().Scope() {
			underlying := describePayload(named.Underlying(), seen, depth+1, work)
			return payloadType{Kind: payloadNamed, Package: object.Pkg().Path(), Name: object.Name(), Fields: underlying.Fields}
		}
	}
	switch t := t.Underlying().(type) {
	case *types.Pointer:
		return payloadType{Kind: payloadPointer, Fields: []payloadField{{Type: describePayload(t.Elem(), seen, depth+1, work)}}}
	case *types.Slice:
		return payloadType{Kind: payloadCollection, Fields: []payloadField{{Type: describePayload(t.Elem(), seen, depth+1, work)}}}
	case *types.Array:
		if t.Len() != 0 {
			return payloadType{Kind: payloadCollection, Fields: []payloadField{{Type: describePayload(t.Elem(), seen, depth+1, work)}}}
		}
	case *types.Map:
		return payloadType{Kind: payloadCollection, Fields: []payloadField{
			{Type: describePayload(t.Key(), seen, depth+1, work)},
			{Type: describePayload(t.Elem(), seen, depth+1, work)},
		}}
	case *types.Struct:
		result := payloadType{Kind: payloadStruct}
		for field := range t.Fields() {
			if work.exhausted() {
				break
			}
			result.Fields = append(result.Fields, payloadField{field.Name(), describePayload(field.Type(), seen, depth+1, work)})
		}
		return result
	}
	return payloadType{}
}

func mergePayloads(existing, added []payloadType) []payloadType {
	for _, value := range added {
		if len(existing) >= maxPayloadAlternatives {
			break
		}
		found := false
		for _, previous := range existing {
			if reflect.DeepEqual(previous, value) {
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, value)
		}
	}
	return existing
}

func comparePayloads(first, second payloadType) int {
	if order := cmp.Or(cmp.Compare(first.Kind, second.Kind), cmp.Compare(first.Package, second.Package), cmp.Compare(first.Name, second.Name)); order != 0 {
		return order
	}
	return slices.CompareFunc(first.Fields, second.Fields, func(a, b payloadField) int {
		if order := cmp.Compare(a.Name, b.Name); order != 0 {
			return order
		}
		return comparePayloads(a.Type, b.Type)
	})
}
