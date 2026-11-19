package domainconstruct

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"reflect"

	"golang.org/x/tools/go/analysis"
)

// Contract inference uses syntax and type information so importing a contract
// does not require building SSA for every dependency, including the standard
// library. Declared serialization boundaries are kept separate from inferred
// body effects: a declaration describes a contract, not a proof of its body.
var contractsAnalyzer = &analysis.Analyzer{
	Name:       "domaincontracts",
	Doc:        "derive error and construction contracts for domain zero analysis",
	Run:        runContracts,
	ResultType: reflect.TypeFor[*contractSet](),
	FactTypes:  []analysis.Fact{new(functionContract), new(sentinelContract), new(boundaryFact), new(presenceFact)},
}

type contractSet struct {
	functions           map[*types.Func]*functionContract
	boundaries          map[*types.Func]*boundaryFact
	boundaryDiagnostics []analysis.Diagnostic
	presences           map[*types.Func]bool
	presenceDiagnostics []analysis.Diagnostic
	sentinels           map[*types.Var]bool
	writes              map[*types.Var][]token.Pos
}

// A rule describes one error result. Parameter -1 means always non-nil;
// otherwise a non-nil argument guarantees a non-nil result. Exact also proves
// that a nil argument produces nil, permitting wrapped result/error forwarding.
type errorRule struct {
	Result    int
	Parameter int
	Exact     bool
}

type functionContract struct {
	Errors   []errorRule
	Sources  []resultSource
	Payloads []payloadRule
	Lengths  []resultLength
	Effects  []parameterEffect
}

func (*functionContract) AFact() {}

type sentinelContract struct{}

func (*sentinelContract) AFact() {}

type contractBuilder struct {
	pass   *analysis.Pass
	set    *contractSet
	decls  []contractDeclaration
	values map[*types.Var]ast.Expr
}

type contractDeclaration struct {
	function *types.Func
	syntax   *ast.FuncDecl
}

const maxContractRounds = 16

func runContracts(pass *analysis.Pass) (any, error) {
	b := &contractBuilder{
		pass: pass,
		set: &contractSet{
			functions:  make(map[*types.Func]*functionContract),
			boundaries: make(map[*types.Func]*boundaryFact),
			presences:  make(map[*types.Func]bool),
			sentinels:  make(map[*types.Var]bool),
			writes:     make(map[*types.Var][]token.Pos),
		},
		values: make(map[*types.Var]ast.Expr),
	}
	b.collectBoundaries()
	b.collectPresences()
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Body != nil {
					b.decls = append(b.decls, contractDeclaration{
						function: pass.TypesInfo.Defs[declaration.Name].(*types.Func),
						syntax:   declaration,
					})
				}
			case *ast.GenDecl:
				if declaration.Tok == token.VAR {
					for _, spec := range declaration.Specs {
						spec := spec.(*ast.ValueSpec)
						if len(spec.Names) == len(spec.Values) {
							for index, name := range spec.Names {
								object := pass.TypesInfo.Defs[name].(*types.Var)
								if isErrorType(object.Type()) {
									b.values[object] = spec.Values[index]
								}
							}
						}
					}
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.AssignStmt:
				for _, target := range node.Lhs {
					b.recordWrite(target)
				}
			case *ast.RangeStmt:
				if node.Tok == token.ASSIGN {
					b.recordWrite(node.Key)
					b.recordWrite(node.Value)
				}
			case *ast.UnaryExpr:
				if node.Op == token.AND {
					b.recordWrite(node.X)
				}
			}
			return true
		})
	}
	b.inferContracts()
	for object, fact := range b.set.functions {
		if object.Pkg() == pass.Pkg && fact != nil {
			pass.ExportObjectFact(object, fact)
		}
	}
	for object, nonNil := range b.set.sentinels {
		if object.Pkg() == pass.Pkg && nonNil {
			pass.ExportObjectFact(object, new(sentinelContract))
		}
	}
	for _, imported := range pass.AllObjectFacts() {
		switch fact := imported.Fact.(type) {
		case *functionContract:
			b.set.functions[imported.Object.(*types.Func)] = fact
		case *sentinelContract:
			b.set.sentinels[imported.Object.(*types.Var)] = true
		case *boundaryFact:
			b.set.boundaries[imported.Object.(*types.Func).Origin()] = fact
		case *presenceFact:
			b.set.presences[imported.Object.(*types.Func).Origin()] = true
		}
	}
	return b.set, nil
}

func (b *contractBuilder) inferContracts() {
	// Effects depend only on the function body. Compute them once and keep
	// them even when dependent summaries do not reach a fixed point.
	effects := make(map[*types.Func][]parameterEffect)
	for _, declaration := range b.decls {
		if inferred := b.inferEffects(declaration.function, declaration.syntax); len(inferred) != 0 {
			effects[declaration.function] = inferred
		}
	}
	for range maxContractRounds {
		changed := false
		for object, value := range b.values {
			if !b.set.sentinels[object] && len(b.set.writes[object]) == 0 {
				e := nilnessEvaluator{builder: b}
				if e.converted(value, object.Type(), nil) == notZero {
					b.set.sentinels[object] = true
					changed = true
				}
			}
		}
		for _, declaration := range b.decls {
			object, syntax := declaration.function, declaration.syntax
			fact := b.inferErrors(object, syntax)
			if effects := effects[object]; len(effects) != 0 {
				if fact == nil {
					fact = new(functionContract)
				}
				fact.Effects = effects
			}
			if payloads := b.inferPayloads(object, syntax); len(payloads) != 0 {
				if fact == nil {
					fact = new(functionContract)
				}
				fact.Payloads = payloads
			}
			if sources := b.inferSources(object, syntax); len(sources) != 0 {
				if fact == nil {
					fact = new(functionContract)
				}
				fact.Sources = sources
			}
			if lengths := b.inferLengths(object, syntax); len(lengths) != 0 {
				if fact == nil {
					fact = new(functionContract)
				}
				fact.Lengths = lengths
			}
			if !reflect.DeepEqual(b.set.functions[object], fact) {
				if fact == nil {
					delete(b.set.functions, object)
				} else {
					b.set.functions[object] = fact
				}
				changed = true
			}
		}
		if !changed {
			return
		}
	}
	// The last round may contain facts derived from an unstable callee.
	// Discard local dependent summaries together and retain imported facts.
	maps.DeleteFunc(b.set.functions, func(object *types.Func, _ *functionContract) bool { return object.Pkg() == b.pass.Pkg })
	maps.DeleteFunc(b.set.sentinels, func(object *types.Var, _ bool) bool { return object.Pkg() == b.pass.Pkg })
	for object, effect := range effects {
		b.set.functions[object] = &functionContract{Effects: effect}
	}
}

func (b *contractBuilder) recordWrite(expression ast.Expr) {
	if object, ok := expressionObject(b.pass.TypesInfo, expression).(*types.Var); ok &&
		object.Pkg() != nil && object.Parent() == object.Pkg().Scope() && isErrorType(object.Type()) {
		b.set.writes[object] = append(b.set.writes[object], expression.Pos())
	}
}

func (b *contractBuilder) function(object *types.Func) *functionContract {
	if object == nil {
		return nil
	}
	object = object.Origin()
	if fact := b.set.functions[object]; fact != nil {
		return fact
	}
	if object.Pkg() != b.pass.Pkg {
		var fact functionContract
		if b.pass.ImportObjectFact(object, &fact) {
			b.set.functions[object] = &fact
			return &fact
		}
	}
	return nil
}

func (b *contractBuilder) sentinel(object *types.Var) bool {
	if len(b.set.writes[object]) != 0 {
		return false
	}
	if b.set.sentinels[object] {
		return true
	}
	if object.Pkg() != b.pass.Pkg && b.pass.ImportObjectFact(object, new(sentinelContract)) {
		b.set.sentinels[object] = true
		return true
	}
	return false
}

func (b *contractBuilder) inferErrors(object *types.Func, declaration *ast.FuncDecl) *functionContract {
	signature := object.Type().(*types.Signature)
	var rules []errorRule
	for result := range signature.Results().Len() {
		if !isErrorType(signature.Results().At(result).Type()) {
			continue
		}
		e := nilnessEvaluator{builder: b, signature: signature, result: result}
		if e.function(declaration.Body, nil) == notZero {
			rules = append(rules, errorRule{Result: result, Parameter: -1})
			continue
		}
		for parameter := range signature.Params().Len() {
			variable := signature.Params().At(parameter)
			if !isErrorType(variable.Type()) {
				continue
			}
			if e.function(declaration.Body, nilnessEnv{variable: notZero}) == notZero {
				exact := e.function(declaration.Body, nilnessEnv{variable: isZero}) == isZero
				rules = append(rules, errorRule{Result: result, Parameter: parameter, Exact: exact})
			}
		}
	}
	if len(rules) == 0 {
		return nil
	}
	return &functionContract{Errors: rules}
}

func expressionObject(info *types.Info, expression ast.Expr) types.Object {
	switch expression := ast.Unparen(expression).(type) {
	case *ast.Ident:
		return info.ObjectOf(expression)
	case *ast.SelectorExpr:
		return info.ObjectOf(expression.Sel)
	case *ast.IndexExpr:
		return expressionObject(info, expression.X)
	case *ast.IndexListExpr:
		return expressionObject(info, expression.X)
	}
	return nil
}

func callParameter(info *types.Info, call *ast.CallExpr, index int) ast.Expr {
	if function, ok := expressionObject(info, call.Fun).(*types.Func); ok {
		signature := function.Type().(*types.Signature)
		if signature.Variadic() && index == signature.Params().Len()-1 && !call.Ellipsis.IsValid() {
			return nil // The unexpanded slice and its first element are different values.
		}
	}
	expression := ast.Unparen(call.Fun)
	if selector, ok := expression.(*ast.SelectorExpr); ok {
		if selection := info.Selections[selector]; selection != nil {
			switch selection.Kind() {
			case types.MethodExpr:
				index++ // The explicit receiver precedes the declared parameters.
			case types.MethodVal:
				if index == -1 {
					return selector.X
				}
			}
		}
	}
	if index >= 0 && index < len(call.Args) {
		return call.Args[index]
	}
	return nil
}
