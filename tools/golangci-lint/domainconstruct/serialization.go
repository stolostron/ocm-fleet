package domainconstruct

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strings"

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
	callPosition token.Pos
}

type serializationReport struct {
	pos                      token.Pos
	rule, operation, message string
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
					if p := c.policies.declaration(object.Type()); p != nil {
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
		decoder := codecMethod(p.typ, decoderMethod(codec), true)
		if decoder == nil || !validDecoder(decoder.Type().(*types.Signature), codec) {
			invalid("decoder-required", fmt.Sprintf("%s serialization requires a pointer %s method with the standard decoding signature", codec, decoderMethod(codec)))
			continue
		}
		encoderName := map[string]string{"json": "MarshalJSON", "text": "MarshalText", "sql": "Value"}[codec]
		encoder := codecMethod(p.typ, encoderName, false)
		if codec == "json" {
			// JSON can encode primitive representations without a custom hook.
			// Composite domain state needs a value hook, including when nested
			// in a non-addressable interface or map value.
			if _, scalar := p.typ.Underlying().(*types.Basic); scalar {
				continue
			}
			if encoder == nil {
				encoder = codecMethod(p.typ, "MarshalText", false)
			}
		}
		if encoder == nil || !validEncoder(encoder.Type().(*types.Signature), codec) {
			invalid("encoder-required", fmt.Sprintf("%s serialization requires a value %s method with the standard encoding signature", codec, encoderName))
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
				for _, argument := range c.arguments(call, function, index) {
					c.checkExpression(argument, boundary.Codec, selection.operation)
				}
			}
		}
		signature, _ := c.pass.TypesInfo.TypeOf(call.Fun).(*types.Signature)
		if signature != nil {
			for _, index := range boundary.Results {
				if index < signature.Results().Len() {
					c.checkType(call, signature.Results().At(index).Type(), boundary.Codec, "result")
				}
			}
		}
		if identifier := calledIdentifier(call.Fun); identifier != nil {
			instance := c.pass.TypesInfo.Instances[identifier]
			for _, index := range boundary.TypeParameters {
				if index < instance.TypeArgs.Len() {
					c.checkType(call, instance.TypeArgs.At(index), boundary.Codec, "type-argument")
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
	signature := function.Type().(*types.Signature)
	offset := 0
	if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
		if selection := c.pass.TypesInfo.Selections[selector]; selection != nil && selection.Kind() == types.MethodExpr {
			offset = 1
		}
	}
	if index == -1 {
		if offset == 1 {
			return call.Args[:1]
		}
		if receiver := selectedReceiver(call); receiver != nil {
			return []ast.Expr{receiver}
		}
		return nil
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
	if call, ok := expression.(*ast.CallExpr); ok && c.pass.TypesInfo.Types[call.Fun].IsType() && len(call.Args) == 1 && emptyInterface(c.pass.TypesInfo.TypeOf(call)) {
		c.checkExpression(call.Args[0], codec, operation)
		return
	}
	c.checkType(expression, c.pass.TypesInfo.TypeOf(expression), codec, operation)
}

func (c *serializationChecker) checkType(node ast.Node, t types.Type, codec, operation string) {
	if t == nil {
		return
	}
	if problem := c.encodingProblem(t, codec, make(map[encodingVisit]bool)); problem != nil {
		path := ""
		if problem.path != "" {
			path = "at path " + problem.path + ": "
		}
		c.report(node, "unsupported-codec", operation, "codec-not-declared",
			fmt.Sprintf("%s%s does not declare %s serialization; use a snapshot or declare a checked native round trip", path, typeName(problem.typ), problem.codec),
			problem.typ, problem.path)
	}
}

func (c *serializationChecker) encodingProblem(t types.Type, codec string, seen map[encodingVisit]bool) *serializationProblem {
	visit := encodingVisit{t, codec}
	if seen[visit] {
		return nil
	}
	seen[visit] = true
	if p := c.policies.lookup(t); p != nil {
		if p.supports(codec) {
			return nil
		}
		return &serializationProblem{typ: t, codec: codec}
	}
	switch shape := t.Underlying().(type) {
	case *types.Pointer:
		return c.encodingProblem(shape.Elem(), codec, seen)
	case *types.Slice:
		return c.encodingProblem(shape.Elem(), codec, seen)
	case *types.Array:
		return c.encodingProblem(shape.Elem(), codec, seen)
	case *types.Map:
		keyCodec := codec
		if codec == "json" {
			keyCodec = "text"
		}
		if problem := c.encodingProblem(shape.Key(), keyCodec, seen); problem != nil {
			problem.path = fieldPath("[key]", problem.path)
			return problem
		}
		return c.encodingProblem(shape.Elem(), codec, seen)
	case *types.Struct:
		if standardPlainType(t) {
			return nil
		}
		for index := range shape.NumFields() {
			field := shape.Field(index)
			if !field.Exported() {
				continue
			}
			if codec == "json" && strings.Split(reflect.StructTag(shape.Tag(index)).Get("json"), ",")[0] == "-" {
				continue
			}
			if problem := c.encodingProblem(field.Type(), codec, seen); problem != nil {
				problem.path = fieldPath(field.Name(), problem.path)
				return problem
			}
		}
	}
	return nil
}

func (c *serializationChecker) snapshotProblem(t types.Type, seen map[types.Type]bool) *serializationProblem {
	if seen[t] {
		return nil
	}
	seen[t] = true
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
		for index := range shape.NumFields() {
			field := shape.Field(index)
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
