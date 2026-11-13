package domainconstruct

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
)

type resultLength struct {
	Result int
	Length sourceLength
}

// Length facts describe only the returned slice header, independently of its
// contents. Filling its elements or updating a cursor does not change its length.
type lengthEvaluator struct {
	builder     *contractBuilder
	signature   *types.Signature
	definitions map[*types.Var]ast.Expr
	writes      map[*types.Var]int
	escaped     map[*types.Var]bool
	initializes map[*types.Var]bool
	returns     []*ast.ReturnStmt
	firstReturn token.Pos
	jumping     bool
}

func (b *contractBuilder) inferLengths(object *types.Func, declaration *ast.FuncDecl) []resultLength {
	signature := object.Type().(*types.Signature)
	var results []int
	for index := range signature.Results().Len() {
		if _, ok := signature.Results().At(index).Type().Underlying().(*types.Slice); ok {
			results = append(results, index)
		}
	}
	if len(results) == 0 {
		return nil
	}
	e := lengthEvaluator{
		builder: b, signature: signature,
		definitions: make(map[*types.Var]ast.Expr),
		writes:      make(map[*types.Var]int), escaped: make(map[*types.Var]bool),
		initializes: make(map[*types.Var]bool),
	}
	// A unique assignment inside a branch need not run. Accept local header
	// definitions only when they occur unconditionally before every return.
	for _, statement := range declaration.Body.List {
		switch statement := statement.(type) {
		case *ast.AssignStmt:
			if len(statement.Rhs) == len(statement.Lhs) {
				for _, target := range statement.Lhs {
					if variable, ok := expressionObject(b.pass.TypesInfo, target).(*types.Var); ok {
						e.initializes[variable] = true
					}
				}
			}
		case *ast.DeclStmt:
			if declaration, ok := statement.Decl.(*ast.GenDecl); ok {
				for _, spec := range declaration.Specs {
					if spec, ok := spec.(*ast.ValueSpec); ok && len(spec.Values) == len(spec.Names) {
						for _, name := range spec.Names {
							if variable, ok := b.pass.TypesInfo.Defs[name].(*types.Var); ok {
								e.initializes[variable] = true
							}
						}
					}
				}
			}
		}
	}
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			// Captures may rewrite a header or its size after its allocation,
			// including in a deferred callback after a named return.
			ast.Inspect(node.Body, func(node ast.Node) bool {
				if identifier, ok := node.(*ast.Ident); ok {
					if variable, ok := expressionObject(b.pass.TypesInfo, identifier).(*types.Var); ok {
						e.escaped[variable] = true
					}
				}
				return true
			})
			return false
		case *ast.AssignStmt:
			for index, target := range node.Lhs {
				var source ast.Expr
				if node.Tok == token.ASSIGN || node.Tok == token.DEFINE {
					if len(node.Rhs) == len(node.Lhs) {
						source = node.Rhs[index]
					}
				}
				e.recordWrite(target, source)
			}
		case *ast.ValueSpec:
			for index, name := range node.Names {
				if len(node.Values) == len(node.Names) {
					e.recordWrite(name, node.Values[index])
				}
			}
		case *ast.IncDecStmt:
			e.recordWrite(node.X, nil)
		case *ast.RangeStmt:
			if node.Key != nil {
				e.recordWrite(node.Key, nil)
			}
			if node.Value != nil {
				e.recordWrite(node.Value, nil)
			}
		case *ast.UnaryExpr:
			if node.Op == token.AND {
				if identifier, ok := ast.Unparen(node.X).(*ast.Ident); ok {
					if variable, ok := expressionObject(b.pass.TypesInfo, identifier).(*types.Var); ok {
						e.escaped[variable] = true
					}
				}
			}
		case *ast.SelectorExpr:
			if selection := b.pass.TypesInfo.Selections[node]; selection != nil && selection.Kind() == types.MethodVal {
				if signature, ok := selection.Obj().Type().(*types.Signature); ok && signature.Recv() != nil {
					if _, pointer := signature.Recv().Type().Underlying().(*types.Pointer); pointer {
						if variable, ok := expressionObject(b.pass.TypesInfo, ast.Unparen(node.X)).(*types.Var); ok {
							e.escaped[variable] = true
						}
					}
				}
			}
		case *ast.ReturnStmt:
			e.returns = append(e.returns, node)
			if e.firstReturn == token.NoPos || node.Pos() < e.firstReturn {
				e.firstReturn = node.Pos()
			}
		case *ast.BranchStmt:
			if node.Tok == token.GOTO {
				e.jumping = true
			}
		}
		return true
	})
	var lengths []resultLength
	for _, result := range results {
		var common sourceLength
		known := len(e.returns) != 0 && !e.jumping
		for index, statement := range e.returns {
			var length sourceLength
			var ok bool
			if len(statement.Results) == signature.Results().Len() {
				length, ok = e.slice(statement.Results[result], 0)
			} else if len(statement.Results) == 0 {
				length, ok = e.variable(signature.Results().At(result), true, 0)
			}
			if !ok || index > 0 && common != length {
				known = false
				break
			}
			common = length
		}
		if known {
			lengths = append(lengths, resultLength{result, common})
		}
	}
	return lengths
}

func (e *lengthEvaluator) recordWrite(target, source ast.Expr) {
	if identifier, ok := ast.Unparen(target).(*ast.Ident); ok {
		if variable, ok := expressionObject(e.builder.pass.TypesInfo, identifier).(*types.Var); ok {
			e.writes[variable]++
			e.definitions[variable] = source
		}
	}
}

func (e *lengthEvaluator) variable(variable *types.Var, fromLen bool, depth int) (sourceLength, bool) {
	if e.escaped[variable] || depth > 8 {
		return sourceLength{}, false
	}
	for index := range e.signature.Params().Len() {
		if e.signature.Params().At(index) == variable && e.writes[variable] == 0 {
			return sourceLength{Constant: -1, Parameter: index, FromLen: fromLen}, true
		}
	}
	if e.writes[variable] == 1 && e.initializes[variable] && e.definitions[variable] != nil && e.definitions[variable].Pos() < e.firstReturn {
		if fromLen {
			return e.slice(e.definitions[variable], depth+1)
		}
		return e.length(e.definitions[variable], depth+1)
	}
	return sourceLength{}, false
}

func (e *lengthEvaluator) slice(expression ast.Expr, depth int) (sourceLength, bool) {
	if depth > 8 {
		return sourceLength{}, false
	}
	expression = ast.Unparen(expression)
	if e.builder.pass.TypesInfo.Types[expression].IsNil() {
		return sourceLength{Constant: 0, Parameter: -1}, true
	}
	if _, slice := e.builder.pass.TypesInfo.TypeOf(expression).Underlying().(*types.Slice); !slice {
		return sourceLength{}, false
	}
	if identifier, ok := expression.(*ast.Ident); ok {
		if variable, ok := expressionObject(e.builder.pass.TypesInfo, identifier).(*types.Var); ok {
			return e.variable(variable, true, depth+1)
		}
	}
	if call, ok := expression.(*ast.CallExpr); ok {
		if builtin, ok := expressionObject(e.builder.pass.TypesInfo, call.Fun).(*types.Builtin); ok && builtin.Name() == "make" && len(call.Args) >= 2 {
			return e.length(call.Args[1], depth+1)
		}
		if function, ok := expressionObject(e.builder.pass.TypesInfo, call.Fun).(*types.Func); ok {
			if fact := e.builder.function(function); fact != nil {
				for _, result := range fact.Lengths {
					if result.Result != 0 {
						continue
					}
					if result.Length.Parameter < 0 {
						return result.Length, true
					}
					if argument := callParameter(e.builder.pass.TypesInfo, call, result.Length.Parameter); argument != nil {
						if result.Length.FromLen {
							return e.slice(argument, depth+1)
						}
						return e.length(argument, depth+1)
					}
				}
			}
		}
	}
	return sourceLength{}, false
}

func (e *lengthEvaluator) length(expression ast.Expr, depth int) (sourceLength, bool) {
	if depth > 8 {
		return sourceLength{}, false
	}
	expression = ast.Unparen(expression)
	if number := e.builder.pass.TypesInfo.Types[expression].Value; number != nil && number.Kind() == constant.Int {
		if value, ok := constant.Int64Val(number); ok && value >= 0 {
			return sourceLength{Constant: value, Parameter: -1}, true
		}
	}
	if identifier, ok := expression.(*ast.Ident); ok {
		if variable, ok := expressionObject(e.builder.pass.TypesInfo, identifier).(*types.Var); ok {
			return e.variable(variable, false, depth+1)
		}
	}
	if call, ok := expression.(*ast.CallExpr); ok && len(call.Args) == 1 {
		if builtin, ok := expressionObject(e.builder.pass.TypesInfo, call.Fun).(*types.Builtin); ok && builtin.Name() == "len" {
			return e.slice(call.Args[0], depth+1)
		}
	}
	return sourceLength{}, false
}
