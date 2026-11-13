package domainconstruct

import (
	"go/ast"
	"go/types"
	"reflect"
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
	hasError := false
	var rules []payloadRule
	for index := range signature.Results().Len() {
		t := signature.Results().At(index).Type()
		if isErrorType(t) {
			hasError = true
		} else if _, boxed := t.Underlying().(*types.Interface); boxed {
			rules = append(rules, payloadRule{Result: index})
		}
	}
	if !hasError || len(rules) == 0 {
		return nil
	}
	environment := make(map[*types.Var][]payloadType)
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ValueSpec:
			for index, name := range node.Names {
				if len(node.Values) == len(node.Names) {
					variable, _ := b.pass.TypesInfo.Defs[name].(*types.Var)
					environment[variable] = mergePayloads(environment[variable], b.payloadExpression(node.Values[index], 0, environment))
				}
			}
		case *ast.AssignStmt:
			for index, target := range node.Lhs {
				if variable, ok := expressionObject(b.pass.TypesInfo, target).(*types.Var); ok {
					var values []payloadType
					if len(node.Rhs) == len(node.Lhs) {
						values = b.payloadExpression(node.Rhs[index], 0, environment)
					} else if len(node.Rhs) == 1 {
						values = b.payloadExpression(node.Rhs[0], index, environment)
					}
					environment[variable] = mergePayloads(environment[variable], values)
				}
			}
		case *ast.ReturnStmt:
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
			result = append(result, rule)
		}
	}
	return result
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
	if _, boxed := t.Underlying().(*types.Interface); !boxed {
		if described := describePayload(t, make(map[types.Type]bool), 0); described.Kind != payloadUnknown {
			return []payloadType{described}
		}
	}
	if variable, ok := expressionObject(b.pass.TypesInfo, expression).(*types.Var); ok {
		return environment[variable]
	}
	if call, ok := expression.(*ast.CallExpr); ok {
		if b.pass.TypesInfo.Types[call.Fun].IsType() && len(call.Args) == 1 {
			return b.payloadExpression(call.Args[0], 0, environment)
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

func describePayload(t types.Type, seen map[types.Type]bool, depth int) payloadType {
	if seen[t] || depth > 8 {
		return payloadType{}
	}
	seen[t] = true
	defer delete(seen, t)
	t = types.Unalias(t)
	if named, ok := t.(*types.Named); ok {
		result := payloadType{Kind: payloadNamed, Name: named.Obj().Name()}
		if named.Obj().Pkg() != nil {
			result.Package = named.Obj().Pkg().Path()
		}
		underlying := describePayload(named.Underlying(), seen, depth+1)
		result.Fields = underlying.Fields
		return result
	}
	switch t := t.Underlying().(type) {
	case *types.Pointer:
		return payloadType{Kind: payloadPointer, Fields: []payloadField{{Type: describePayload(t.Elem(), seen, depth+1)}}}
	case *types.Slice:
		return payloadType{Kind: payloadCollection, Fields: []payloadField{{Type: describePayload(t.Elem(), seen, depth+1)}}}
	case *types.Array:
		if t.Len() != 0 {
			return payloadType{Kind: payloadCollection, Fields: []payloadField{{Type: describePayload(t.Elem(), seen, depth+1)}}}
		}
	case *types.Map:
		return payloadType{Kind: payloadCollection, Fields: []payloadField{
			{Type: describePayload(t.Key(), seen, depth+1)},
			{Type: describePayload(t.Elem(), seen, depth+1)},
		}}
	case *types.Struct:
		result := payloadType{Kind: payloadStruct}
		for index := range t.NumFields() {
			field := t.Field(index)
			result.Fields = append(result.Fields, payloadField{field.Name(), describePayload(field.Type(), seen, depth+1)})
		}
		return result
	}
	return payloadType{}
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
			existing = append(existing, value)
		}
	}
	return existing
}
