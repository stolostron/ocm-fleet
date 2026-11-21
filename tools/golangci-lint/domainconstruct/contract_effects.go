package domainconstruct

import (
	"go/ast"
	"go/token"
	"go/types"
)

type mutationKind uint8

const (
	mutationUnknown mutationKind = iota
	mutationReadOnly
	mutationZero
	mutationNonzero
)

// Only small, unconditional scalar writes and demonstrably read-only uses are
// summarized. Conditional writes, aliases, escaping addresses, and unsupported
// bodies remain possible mutations; a function's name grants no contract.
type parameterEffect struct {
	Parameter        int
	Kind             mutationKind
	LocalStorageOnly bool
	LastWrite        int
	Publishes        bool
}

func (fact *functionContract) effectForParameter(index int) parameterEffect {
	if fact != nil {
		for _, effect := range fact.Effects {
			if effect.Parameter == index {
				return effect
			}
		}
	}
	return parameterEffect{}
}

func (b *contractBuilder) inferEffects(object *types.Func, declaration *ast.FuncDecl) []parameterEffect {
	signature := object.Type().(*types.Signature)
	var effects []parameterEffect
	for index := -1; index < signature.Params().Len(); index++ {
		var parameter *types.Var
		if index == -1 {
			parameter = signature.Recv()
		} else {
			parameter = signature.Params().At(index)
		}
		if parameter == nil {
			continue
		}
		if _, scalar := parameter.Type().Underlying().(*types.Basic); scalar {
			continue // Scalar arguments cannot expose mutable caller storage.
		}
		effect := b.parameterEffect(parameter, declaration.Body)
		if effect.Kind != mutationUnknown || effect.Publishes {
			effect.Parameter = index
			effects = append(effects, effect)
		}
	}
	return effects
}

func (b *contractBuilder) parameterEffect(parameter *types.Var, body *ast.BlockStmt) parameterEffect {
	effect := parameterEffect{Kind: mutationReadOnly}
	// Record only direct writes in the function's top-level statement list.
	// Early exits and control flow prevent claiming a definite postcondition.
	writes := make(map[*ast.StarExpr]mutationKind)
	straight := true
	var lastWrite token.Pos
	for _, statement := range body.List {
		switch statement := statement.(type) {
		case *ast.AssignStmt:
			if straight && statement.Tok == token.ASSIGN && len(statement.Lhs) == 1 && len(statement.Rhs) == 1 {
				if target, ok := ast.Unparen(statement.Lhs[0]).(*ast.StarExpr); ok && expressionObject(b.pass.TypesInfo, target.X) == parameter {
					if value := b.pass.TypesInfo.Types[statement.Rhs[0]].Value; value != nil {
						write := mutationNonzero
						if constantZero(value) {
							write = mutationZero
						}
						writes[target] = write
						lastWrite = statement.End()
					}
				}
			}
		case *ast.DeclStmt, *ast.EmptyStmt:
		default:
			straight = false
		}
	}
	var ancestors []ast.Node
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil {
			ancestors = ancestors[:len(ancestors)-1]
			return true
		}
		if call, ok := node.(*ast.CallExpr); ok && lastWrite.IsValid() && call.Pos() >= lastWrite &&
			!b.pass.TypesInfo.Types[call.Fun].IsType() {
			effect.Kind = mutationUnknown
		}
		if assignment, ok := node.(*ast.AssignStmt); ok {
			for index, target := range assignment.Lhs {
				if variable, ok := expressionObject(b.pass.TypesInfo, target).(*types.Var); ok &&
					variable.Pkg() != nil && variable.Parent() == variable.Pkg().Scope() {
					if !lastWrite.IsValid() || assignment.Pos() >= lastWrite {
						effect.LocalStorageOnly = true // A borrowed argument may address the same global.
					}
					if len(assignment.Rhs) == len(assignment.Lhs) && expressionObject(b.pass.TypesInfo, assignment.Rhs[index]) == parameter {
						effect.Publishes = true
					}
				}
			}
		}
		if id, ok := node.(*ast.Ident); ok && b.pass.TypesInfo.ObjectOf(id) == parameter {
			parent := ancestors[len(ancestors)-1]
			readOnly := false
			switch parent := parent.(type) {
			case *ast.StarExpr:
				if _, scalar := b.pass.TypesInfo.TypeOf(parent).Underlying().(*types.Basic); scalar {
					readOnly = true
					outer := len(ancestors) - 2
					for outer >= 0 {
						if _, parenthesized := ancestors[outer].(*ast.ParenExpr); !parenthesized {
							break
						}
						outer--
					}
					if outer >= 0 {
						switch enclosing := ancestors[outer].(type) {
						case *ast.AssignStmt:
							for _, target := range enclosing.Lhs {
								if ast.Unparen(target) == parent {
									readOnly = false
									if write, ok := writes[parent]; ok && effect.Kind != mutationUnknown {
										effect.Kind, readOnly = write, true
									}
								}
							}
						case *ast.IncDecStmt:
							readOnly = false
						case *ast.RangeStmt:
							readOnly = ast.Unparen(enclosing.Key) != parent && ast.Unparen(enclosing.Value) != parent
						case *ast.UnaryExpr:
							readOnly = enclosing.Op != token.AND
						case *ast.SelectorExpr:
							if selection := b.pass.TypesInfo.Selections[enclosing]; selection != nil && selection.Kind() == types.MethodVal {
								receiver := selection.Obj().Type().(*types.Signature).Recv()
								_, pointer := receiver.Type().Underlying().(*types.Pointer)
								readOnly = !pointer
							}
						}
					}
				}
			case *ast.BinaryExpr:
				readOnly = parent.Op == token.EQL || parent.Op == token.NEQ
			case *ast.CallExpr:
				if builtin, ok := expressionObject(b.pass.TypesInfo, parent.Fun).(*types.Builtin); ok {
					readOnly = builtin.Name() == "len" || builtin.Name() == "cap"
				}
			}
			if !readOnly {
				effect.Kind = mutationUnknown
			}
		}
		ancestors = append(ancestors, node)
		return true
	})
	if effect.Kind == mutationZero || effect.Kind == mutationNonzero {
		effect.LastWrite = int(lastWrite - body.Pos())
	}
	return effect
}
