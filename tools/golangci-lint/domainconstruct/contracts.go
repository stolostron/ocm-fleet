package domainconstruct

import (
	"go/ast"
	"go/token"
	"go/types"
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

// A rule describes one error result. A non-nil argument guarantees a non-nil
// result. Parameter -1 makes the rule independent of the arguments. Exact
// preserves nil, and BoxedZeroType identifies a zero of a type parameter whose
// conversion to an interface determines the result when the argument is nil.
type errorRule struct {
	Result        int
	Parameter     int
	Exact         bool
	BoxedZeroType int // The one-based index in the function's type parameters.
}

func (rule errorRule) resultTruth(argument truth, typeArgument types.Type) truth {
	if rule.Parameter >= 0 && argument == notZero {
		return notZero
	}
	if rule.BoxedZeroType != 0 {
		boxed := boxedZeroTruth(typeArgument)
		if boxed == notZero || rule.Parameter < 0 || argument == isZero {
			return boxed
		}
		return unknown
	}
	if rule.Parameter < 0 {
		return notZero
	}
	if rule.Exact && argument == isZero {
		return isZero
	}
	return unknown
}

func boxedZeroTruth(t types.Type) truth {
	if t == nil {
		return unknown
	}
	if _, parameter := types.Unalias(t).(*types.TypeParam); parameter {
		return unknown
	}
	if interfaceValueType(t) {
		return isZero
	}
	return notZero
}

type functionContract struct {
	Errors        []errorRule
	Sources       []resultSource
	FailedSources []resultSource
	Payloads      []payloadRule
	Lengths       []resultLength
	Effects       []parameterEffect
}

func (*functionContract) AFact() {}

type sentinelContract struct{}

func (*sentinelContract) AFact() {}

type contractBuilder struct {
	pass             *analysis.Pass
	set              *contractSet
	decls            []contractDeclaration
	sentinels        []sentinelDeclaration
	pending          map[types.Object]bool
	dependsOnPending bool
}

type contractDeclaration struct {
	function *types.Func
	syntax   *ast.FuncDecl
}

type sentinelDeclaration struct {
	variable *types.Var
	value    ast.Expr
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
									b.sentinels = append(b.sentinels, sentinelDeclaration{object, spec.Values[index]})
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
	b.pending = make(map[types.Object]bool, len(b.decls)+len(b.sentinels))
	for _, declaration := range b.decls {
		b.pending[declaration.function] = true
		if inferred := b.inferEffects(declaration.function, declaration.syntax); len(inferred) != 0 {
			effects[declaration.function] = inferred
		}
	}
	for _, declaration := range b.sentinels {
		if len(b.set.writes[declaration.variable]) == 0 {
			b.pending[declaration.variable] = true
		}
	}
	for range maxContractRounds {
		changed := false
		for _, declaration := range b.sentinels {
			object := declaration.variable
			if !b.pending[object] {
				continue
			}
			b.dependsOnPending = false
			work := workBudget(maxInferenceWork)
			e := nilnessEvaluator{builder: b, complete: true, work: &work}
			nonNil := e.converted(declaration.value, object.Type(), nil) == notZero && e.complete
			if !b.dependsOnPending {
				delete(b.pending, object)
			}
			if b.set.sentinels[object] != nonNil {
				b.set.sentinels[object] = nonNil
				changed = true
			}
		}
		for _, declaration := range b.decls {
			object, syntax := declaration.function, declaration.syntax
			if !b.pending[object] {
				continue
			}
			b.dependsOnPending = false
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
			if sources, failed := b.inferSources(object, syntax); len(sources)+len(failed) != 0 {
				if fact == nil {
					fact = new(functionContract)
				}
				fact.Sources = sources
				fact.FailedSources = failed
			}
			if lengths := b.inferLengths(object, syntax); len(lengths) != 0 {
				if fact == nil {
					fact = new(functionContract)
				}
				fact.Lengths = lengths
			}
			if !b.dependsOnPending {
				delete(b.pending, object)
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
	// A pending lookup can change even when its current answer is empty.
	// Retain summaries only after all the facts they read have settled.
	for object := range b.pending {
		switch object := object.(type) {
		case *types.Var:
			delete(b.set.sentinels, object)
		case *types.Func:
			if effect := effects[object]; len(effect) != 0 {
				b.set.functions[object] = &functionContract{Effects: effect}
			} else {
				delete(b.set.functions, object)
			}
		}
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
	b.dependsOnPending = b.dependsOnPending || b.pending[object]
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
	b.dependsOnPending = b.dependsOnPending || b.pending[object]
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
		switch e.function(declaration.Body, nil) {
		case notZero:
			rules = append(rules, errorRule{Result: result, Parameter: -1})
			continue
		case boxedTypeZero:
			rules = append(rules, errorRule{Result: result, Parameter: -1, BoxedZeroType: e.boxedZeroType})
			continue
		}
		for parameter := range signature.Params().Len() {
			variable := signature.Params().At(parameter)
			if !isErrorType(variable.Type()) {
				continue
			}
			if e.function(declaration.Body, nilnessEnv{variable: notZero}) == notZero {
				rule := errorRule{Result: result, Parameter: parameter}
				switch e.function(declaration.Body, nilnessEnv{variable: isZero}) {
				case isZero:
					rule.Exact = true
				case boxedTypeZero:
					rule.BoxedZeroType = e.boxedZeroType
				}
				rules = append(rules, rule)
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

type boundParameter struct {
	expression ast.Expr
	actual     types.Type
	formal     types.Type
	projection []int // Fields traversed to reach an embedded receiver.
	result     int   // -1 for a single-valued expression.
}

func calledTypeArgument(info *types.Info, call *ast.CallExpr, index int) types.Type {
	if index > 0 {
		instance := info.Instances[calledIdentifier(ast.Unparen(call.Fun))]
		if index <= instance.TypeArgs.Len() {
			return instance.TypeArgs.At(index - 1)
		}
	}
	return nil
}

// A zero index means the type is not one of this function's type parameters.
func declaredTypeParameter(t types.Type, signature *types.Signature) int {
	if parameter, ok := types.Unalias(t).(*types.TypeParam); ok && signature != nil {
		index := parameter.Index()
		parameters := signature.TypeParams()
		if index >= 0 && index < parameters.Len() && parameters.At(index) == parameter {
			return index + 1
		}
	}
	return 0
}

func callParameter(info *types.Info, call *ast.CallExpr, index int) boundParameter {
	bound := boundParameter{result: -1}
	expression := ast.Unparen(call.Fun)
	if selector, ok := expression.(*ast.SelectorExpr); ok {
		if selection := info.Selections[selector]; selection != nil {
			switch selection.Kind() {
			case types.MethodVal, types.MethodExpr:
				if index == -1 {
					indices := selection.Index()
					bound.projection = indices[:len(indices)-1]
					bound.actual = selection.Obj().Type().(*types.Signature).Recv().Type()
					if selection.Kind() == types.MethodVal {
						bound.expression, bound.formal = selector.X, bound.actual
						return bound
					}
				}
				if selection.Kind() == types.MethodExpr {
					index++ // The explicit receiver precedes the declared parameters.
				}
			}
		}
	}
	signature, ok := info.TypeOf(call.Fun).Underlying().(*types.Signature)
	if !ok || index < 0 || index >= signature.Params().Len() {
		return boundParameter{}
	}
	if signature.Variadic() && index == signature.Params().Len()-1 && !call.Ellipsis.IsValid() {
		return boundParameter{} // A packed slice differs from its individual arguments.
	}
	bound.formal = signature.Params().At(index).Type()
	if len(call.Args) == 1 {
		if tuple, ok := info.TypeOf(call.Args[0]).(*types.Tuple); ok && index < tuple.Len() {
			bound.expression, bound.result = call.Args[0], index
			if bound.actual == nil {
				bound.actual = tuple.At(index).Type()
			}
			return bound
		}
	}
	if index < len(call.Args) {
		bound.expression = call.Args[index]
		if bound.actual == nil {
			bound.actual = info.TypeOf(bound.expression)
		}
		return bound
	}
	return boundParameter{}
}
