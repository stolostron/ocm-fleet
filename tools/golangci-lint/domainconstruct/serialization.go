package domainconstruct

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/diagnostic"
)

// SerializationAnalyzer checks declared codecs at serialization boundaries and
// keeps snapshots recursively free of constructor-protected domain values.
var SerializationAnalyzer = &analysis.Analyzer{
	Name:     "domainserialization",
	Doc:      "require explicit domain serialization contracts and plain snapshots",
	Requires: []*analysis.Analyzer{Analyzer, contractsAnalyzer, buildssa.Analyzer},
	Run:      runSerialization,
}

type serializationChecker struct {
	pass         *analysis.Pass
	policies     policySet
	contracts    *contractSet
	calls        map[token.Pos]*ast.CallExpr
	reported     map[serializationReport]bool
	jsonShape    jsonFieldCache
	callPosition token.Pos
}

type serializationReport struct {
	pos                      token.Pos
	rule, operation, message string
}

type encodingOperand struct {
	node                   ast.Node
	codec, operation, path string
	use                    valueUse
	defaultJSONDecode      bool
}

func newEncodingOperand(node ast.Node, codec, operation string) encodingOperand {
	operand := encodingOperand{node: node, codec: codec, operation: operation}
	if codec == "json" {
		operand.use = jsonFields
	}
	return operand
}

func (operand encodingOperand) forType(t types.Type) encodingOperand {
	if operand.operation == "encode" {
		operand.use = jsonUseForType(t, operand.use)
	}
	return operand
}

type serializationProblem struct {
	typ    types.Type
	path   string
	codec  string
	reason string
}

type encodingVisit struct {
	typ   types.Type
	codec string
	use   valueUse
}

func runSerialization(pass *analysis.Pass) (any, error) {
	c := &serializationChecker{
		pass: pass, policies: pass.ResultOf[Analyzer].(policySet), contracts: pass.ResultOf[contractsAnalyzer].(*contractSet),
		calls: make(map[token.Pos]*ast.CallExpr), reported: make(map[serializationReport]bool),
	}
	for _, finding := range c.contracts.boundaryDiagnostics {
		pass.Report(finding)
	}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.TypeSpec:
				if object, ok := pass.TypesInfo.Defs[node.Name].(*types.TypeName); ok {
					if p := c.policies[object]; p != nil {
						c.checkDeclaration(node, p)
					}
				}
			case *ast.CallExpr:
				c.calls[node.Lparen] = node
				c.checkCall(node)
			}
			return true
		})
	}
	c.checkBoxedCalls()
	return nil, nil
}

func (c *serializationChecker) checkDeclaration(node *ast.TypeSpec, p *policy) {
	invalid := func(reason, message string) {
		c.report(node, "invalid-policy", "declaration", reason, message, p.typ, "")
	}
	if p.fact.Snapshot {
		if len(p.fact.Constructors)+len(p.fact.Serialization) != 0 {
			invalid("snapshot-domain-policy", "a snapshot cannot also declare domain constructors or native serialization")
			return
		}
		if problem := c.snapshotProblem(p.typ, make(map[types.Type]bool)); problem != nil {
			c.report(node, "snapshot-domain-value", "snapshot", problem.reason,
				fmt.Sprintf("snapshot %s at path %s contains %s; snapshots must contain plain data, without domain values", p.name, problem.path, typeName(problem.typ)),
				problem.typ, problem.path)
		}
	}
	seen := make(map[string]bool)
	for _, codec := range p.fact.Serialization {
		if !knownCodec(codec) {
			invalid("unknown-codec", fmt.Sprintf("unknown serialization codec %s", codec))
			continue
		}
		if seen[codec] {
			invalid("duplicate-codec", fmt.Sprintf("declare %s serialization only once", codec))
			continue
		}
		seen[codec] = true
		if len(p.fact.Constructors) == 0 {
			invalid("constructors-required", "domain:serialization requires domain:constructors")
			continue
		}
		if !supportsNativeDecode(p.typ, codec) {
			invalid("decoder-required", fmt.Sprintf("%s serialization requires a pointer %s method with the standard decoding signature", codec, decoderMethod(codec)))
			continue
		}
		if !supportsNativeEncode(p.typ, codec) {
			invalid("encoder-required", fmt.Sprintf("%s serialization requires a value %s method with the standard encoding signature", codec, encoderMethod(codec)))
		}
	}
}

func (c *serializationChecker) checkCall(call *ast.CallExpr) {
	c.callPosition = call.Lparen
	defer func() { c.callPosition = token.NoPos }()
	function := callFunction(c.pass.TypesInfo, call)
	for _, boundary := range c.contracts.boundariesFor(function, c.policies) {
		for _, selection := range []struct {
			indices   []int
			operation string
		}{{boundary.Encode, "encode"}, {boundary.Decode, "decode"}} {
			for _, index := range selection.indices {
				if index == -1 {
					receiver := callParameter(c.pass.TypesInfo, call, index)
					if receiver.expression != nil {
						c.checkType(newEncodingOperand(receiver.expression, boundary.Codec, selection.operation), receiver.actual)
					}
					continue
				}
				for _, argument := range c.arguments(call, function, index) {
					c.checkExpression(argument, boundary.Codec, selection.operation)
				}
			}
		}
		signature, _ := c.pass.TypesInfo.TypeOf(call.Fun).(*types.Signature)
		if signature != nil {
			for _, index := range boundary.Results {
				if index < signature.Results().Len() {
					c.checkType(newEncodingOperand(call, boundary.Codec, "result"), signature.Results().At(index).Type())
				}
			}
		}
		if identifier := calledIdentifier(call.Fun); identifier != nil {
			instance := c.pass.TypesInfo.Instances[identifier]
			for _, index := range boundary.TypeParameters {
				if index < instance.TypeArgs.Len() {
					c.checkType(newEncodingOperand(call, boundary.Codec, "type-argument"), instance.TypeArgs.At(index))
				}
			}
		}
	}
}

func calledIdentifier(expression ast.Expr) *ast.Ident {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression
	case *ast.SelectorExpr:
		return expression.Sel
	case *ast.IndexExpr:
		return calledIdentifier(expression.X)
	case *ast.IndexListExpr:
		return calledIdentifier(expression.X)
	}
	return nil
}

func (c *serializationChecker) arguments(call *ast.CallExpr, function *types.Func, index int) []ast.Expr {
	if index == -1 {
		if receiver := callParameter(c.pass.TypesInfo, call, index); receiver.expression != nil {
			return []ast.Expr{receiver.expression}
		}
		return nil
	}
	signature := function.Type().(*types.Signature)
	offset := 0
	if selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if selection := c.pass.TypesInfo.Selections[selector]; selection != nil && selection.Kind() == types.MethodExpr {
			offset = 1
		}
	}
	argument := index + offset
	if argument >= len(call.Args) {
		return nil
	}
	if signature.Variadic() && index == signature.Params().Len()-1 {
		return call.Args[argument:]
	}
	return call.Args[argument : argument+1]
}

func (c *serializationChecker) checkExpression(expression ast.Expr, codec, operation string) {
	// Explicit boxing preserves the concrete type at this source boundary.
	if call, ok := ast.Unparen(expression).(*ast.CallExpr); ok && c.pass.TypesInfo.Types[call.Fun].IsType() && len(call.Args) == 1 {
		if _, boxed := c.pass.TypesInfo.TypeOf(call).Underlying().(*types.Interface); boxed {
			c.checkExpression(call.Args[0], codec, operation)
			return
		}
	}
	c.checkType(newEncodingOperand(expression, codec, operation), c.pass.TypesInfo.TypeOf(expression))
}

func (c *serializationChecker) checkType(operand encodingOperand, t types.Type) {
	if t == nil {
		return
	}
	if problem := c.encodingProblem(t, operand, make(map[encodingVisit]bool)); problem != nil {
		problem.path = fieldPath(operand.path, problem.path)
		path := ""
		if problem.path != "" {
			path = "at path " + problem.path + ": "
		}
		if problem.reason == "json-string-map-key" {
			c.report(operand.node, "unsupported-codec", operand.operation, problem.reason,
				fmt.Sprintf("%s%s needs a plain string map key for a checked JSON round trip", path, typeName(problem.typ)),
				problem.typ, problem.path)
			return
		}
		c.report(operand.node, "unsupported-codec", operand.operation, "codec-not-declared",
			fmt.Sprintf("%s%s does not declare %s serialization; use a snapshot or declare a checked native round trip", path, typeName(problem.typ), problem.codec),
			problem.typ, problem.path)
	}
}

func (c *serializationChecker) encodingProblem(t types.Type, operand encodingOperand, seen map[encodingVisit]bool) *serializationProblem {
	operand = operand.forType(t)
	codec := operand.codec
	visit := encodingVisit{t, codec, operand.use}
	if seen[visit] {
		return nil
	}
	seen[visit] = true
	if optionalPayload(t) != nil {
		// Private optional storage is not a wire representation. In
		// particular, encoding/json would silently omit both private fields.
		return &serializationProblem{typ: t, codec: codec}
	}
	if p := c.policies.lookup(t); p != nil {
		if p.supports(codec) {
			return nil
		}
		return &serializationProblem{typ: t, codec: codec}
	}
	switch shape := t.Underlying().(type) {
	case *types.Pointer:
		return c.encodingProblem(shape.Elem(), operand, seen)
	case *types.Slice:
		return c.encodingProblem(shape.Elem(), operand, seen)
	case *types.Array:
		return c.encodingProblem(shape.Elem(), operand, seen)
	case *types.Map:
		key := operand
		if codec == "json" {
			key.codec, key.use = "text", wholeValue
		}
		if problem := c.encodingProblem(shape.Key(), key, seen); problem != nil {
			problem.path = fieldPath("[key]", problem.path)
			return problem
		}
		if codec == "json" && c.policies.lookup(shape.Key()) != nil {
			if key, basic := shape.Key().Underlying().(*types.Basic); basic && key.Info()&types.IsString != 0 {
				return &serializationProblem{typ: shape.Key(), path: "[key]", codec: codec, reason: "json-string-map-key"}
			}
		}
		return c.encodingProblem(shape.Elem(), operand, seen)
	case *types.Struct:
		if standardPlainType(t) {
			return nil
		}
		for selected := range c.jsonShape.fields(t, operand.use.json()) {
			if codec != "json" && !selected.field.Exported() {
				continue
			}
			if problem := c.promotedCodecProblem(shape, selected, codec); problem != nil {
				return problem
			}
			field := operand
			field.use = operand.use.forField(selected)
			if problem := c.encodingProblem(selected.field.Type(), field, seen); problem != nil {
				problem.path = fieldPath(selected.path, problem.path)
				return problem
			}
		}
	}
	return nil
}

func (c *serializationChecker) promotedCodecProblem(shape *types.Struct, selected structField, codec string) *serializationProblem {
	var path string
	for _, index := range selected.indices[:len(selected.indices)-1] {
		field := shape.Field(index)
		path = fieldPath(path, field.Name())
		if p := c.policies.lookup(field.Type()); p != nil && !p.supports(codec) {
			return &serializationProblem{typ: field.Type(), path: path, codec: codec}
		}
		shape = compositeShape(field.Type()).(*types.Struct)
	}
	return nil
}

func (c *serializationChecker) snapshotProblem(t types.Type, seen map[types.Type]bool) *serializationProblem {
	if seen[t] {
		return nil
	}
	seen[t] = true
	if optionalPayload(t) != nil {
		return &serializationProblem{typ: t, reason: "optional-in-snapshot"}
	}
	if c.policies.lookup(t) != nil {
		return &serializationProblem{typ: t, reason: "domain-value-in-snapshot"}
	}
	if standardPlainType(t) {
		return nil
	}
	switch shape := t.Underlying().(type) {
	case *types.Pointer:
		return c.snapshotProblem(shape.Elem(), seen)
	case *types.Slice:
		return c.snapshotProblem(shape.Elem(), seen)
	case *types.Array:
		return c.snapshotProblem(shape.Elem(), seen)
	case *types.Map:
		if problem := c.snapshotProblem(shape.Key(), seen); problem != nil {
			problem.path = fieldPath("[key]", problem.path)
			return problem
		}
		return c.snapshotProblem(shape.Elem(), seen)
	case *types.Struct:
		for field := range shape.Fields() {
			if problem := c.snapshotProblem(field.Type(), seen); problem != nil {
				problem.path = fieldPath(field.Name(), problem.path)
				return problem
			}
		}
		return nil
	case *types.Basic:
		if shape.Kind() != types.UnsafePointer {
			return nil
		}
	}
	return &serializationProblem{typ: t, reason: "non-data-snapshot-field"}
}

func standardPlainType(t types.Type) bool {
	named := domainType(t)
	return named != nil && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "time" && named.Obj().Name() == "Time"
}

func (c *serializationChecker) report(node ast.Node, rule, operation, reason, message string, t types.Type, path string) {
	pos := serializationPosition(node)
	identity := pos
	if c.callPosition.IsValid() {
		identity = c.callPosition
	}
	key := serializationReport{identity, rule, operation, message}
	if c.reported[key] {
		return
	}
	c.reported[key] = true
	c.pass.Report((diagnostic.Finding{
		Rule: "domainserialization." + rule, Certainty: diagnostic.Known,
		Operation: operation, Reason: reason, Value: expressionText(c.pass.Fset, node),
		Type: typeIdentity(t), Path: path, Pos: pos, Message: message,
	}).Diagnostic())
}
