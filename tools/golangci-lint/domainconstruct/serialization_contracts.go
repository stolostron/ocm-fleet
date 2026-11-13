package domainconstruct

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/diagnostic"
)

// boundaryFact describes an API contract independently of inferred body effects.
// Parameter indices exclude receivers; -1 selects a method's receiver.
type boundaryFact struct {
	Boundaries []codecBoundary
}

type codecBoundary struct {
	Codec          string
	Encode         []int
	Decode         []int
	Results        []int
	TypeParameters []int
	ErrorResult    int
}

func (*boundaryFact) AFact() {}

func (b *contractBuilder) collectBoundaries() {
	for _, file := range b.pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			var name *ast.Ident
			var doc *ast.CommentGroup
			switch node := node.(type) {
			case *ast.FuncDecl:
				name, doc = node.Name, node.Doc
			case *ast.Field:
				if _, method := node.Type.(*ast.FuncType); method && len(node.Names) == 1 {
					name, doc = node.Names[0], node.Doc
				}
			}
			if name == nil || doc == nil {
				return true
			}
			function, ok := b.pass.TypesInfo.Defs[name].(*types.Func)
			if !ok {
				return true
			}
			fact := new(boundaryFact)
			valid := true
			for _, comment := range doc.List {
				fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(comment.Text, "//")))
				if len(fields) == 0 || fields[0] != "domain:boundary" {
					continue
				}
				boundary, err := parseBoundary(function.Type().(*types.Signature), fields[1:])
				if err != nil {
					valid = false
					b.set.boundaryDiagnostics = append(b.set.boundaryDiagnostics, (diagnostic.Finding{
						Rule: "domainserialization.invalid-policy", Certainty: diagnostic.Known,
						Operation: "declaration", Value: function.Name(), Type: typeIdentity(function.Type()),
						Reason: "invalid-boundary", Pos: name.Pos(), Message: err.Error(),
					}).Diagnostic())
					continue
				}
				fact.Boundaries = append(fact.Boundaries, boundary)
			}
			if valid && len(fact.Boundaries) != 0 {
				b.set.boundaries[function.Origin()] = fact
				b.pass.ExportObjectFact(function.Origin(), fact)
			}
			return true
		})
	}
}

func parseBoundary(signature *types.Signature, fields []string) (codecBoundary, error) {
	boundary := codecBoundary{ErrorResult: errorResult(signature)}
	if len(fields) == 0 {
		return boundary, fmt.Errorf("domain:boundary requires a codec and at least one selector")
	}
	boundary.Codec = fields[0]
	if !knownCodec(boundary.Codec) {
		return boundary, fmt.Errorf("unknown serialization codec %s", boundary.Codec)
	}
	seen := make(map[string]bool)
	for _, field := range fields[1:] {
		selector, values, ok := strings.Cut(field, "=")
		if !ok || values == "" || seen[selector] {
			return boundary, fmt.Errorf("use each boundary selector once with a nonempty comma-separated list")
		}
		seen[selector] = true
		for _, value := range strings.Split(values, ",") {
			switch selector {
			case "encode", "decode":
				index := parameterIndex(signature, value)
				if index == -2 {
					return boundary, fmt.Errorf("unknown boundary parameter %s", value)
				}
				if selector == "encode" {
					boundary.Encode = append(boundary.Encode, index)
				} else {
					parameter := signature.Recv()
					if index >= 0 {
						parameter = signature.Params().At(index)
					}
					t := parameter.Type()
					if signature.Variadic() && index == signature.Params().Len()-1 {
						t = t.Underlying().(*types.Slice).Elem()
					}
					switch t.Underlying().(type) {
					case *types.Pointer, *types.Interface:
					default:
						return boundary, fmt.Errorf("decode boundary parameter %s must be a pointer or interface destination", value)
					}
					boundary.Decode = append(boundary.Decode, index)
				}
			case "types":
				index := -1
				for i := range signature.TypeParams().Len() {
					if signature.TypeParams().At(i).Obj().Name() == value {
						index = i
					}
				}
				if index < 0 {
					return boundary, fmt.Errorf("unknown boundary type parameter %s", value)
				}
				boundary.TypeParameters = append(boundary.TypeParameters, index)
			case "results":
				index, err := strconv.Atoi(value)
				if err != nil || index < 0 || index >= signature.Results().Len() {
					return boundary, fmt.Errorf("boundary result index %s is out of range", value)
				}
				if isErrorType(signature.Results().At(index).Type()) {
					return boundary, fmt.Errorf("boundary result index %s selects an error, not a payload", value)
				}
				boundary.Results = append(boundary.Results, index)
			default:
				return boundary, fmt.Errorf("unknown boundary selector %s", selector)
			}
		}
	}
	if len(seen) == 0 {
		return boundary, fmt.Errorf("domain:boundary requires at least one selector")
	}
	if len(boundary.Decode) != 0 && boundary.ErrorResult < 0 {
		return boundary, fmt.Errorf("decode boundary requires exactly one error result")
	}
	return boundary, nil
}

func parameterIndex(signature *types.Signature, name string) int {
	for index := range signature.Params().Len() {
		if signature.Params().At(index).Name() == name {
			return index
		}
	}
	if signature.Recv() != nil && signature.Recv().Name() == name {
		return -1
	}
	return -2
}

func errorResult(signature *types.Signature) int {
	result := -1
	for index := range signature.Results().Len() {
		if isErrorType(signature.Results().At(index).Type()) {
			if result >= 0 {
				return -1
			}
			result = index
		}
	}
	return result
}

func knownCodec(codec string) bool { return codec == "json" || codec == "text" || codec == "sql" }

func decoderMethod(codec string) string {
	switch codec {
	case "json":
		return "UnmarshalJSON"
	case "text":
		return "UnmarshalText"
	case "sql":
		return "Scan"
	}
	return ""
}

func codecMethod(t types.Type, name string, pointer bool) *types.Func {
	if name == "" {
		return nil
	}
	if pointer {
		t = types.NewPointer(t)
	}
	method, _, _ := types.LookupFieldOrMethod(t, false, nil, name)
	function, _ := method.(*types.Func)
	return function
}

func validDecoder(signature *types.Signature, codec string) bool {
	if signature.Recv() == nil || signature.Params().Len() != 1 || signature.Results().Len() != 1 || errorResult(signature) != 0 {
		return false
	}
	if _, pointer := signature.Recv().Type().(*types.Pointer); !pointer {
		return false
	}
	if codec == "sql" {
		return emptyInterface(signature.Params().At(0).Type())
	}
	return byteSlice(signature.Params().At(0).Type())
}

func validEncoder(signature *types.Signature, codec string) bool {
	if signature.Params().Len() != 0 || signature.Results().Len() != 2 || errorResult(signature) != 1 {
		return false
	}
	if codec == "sql" {
		named := domainType(signature.Results().At(0).Type())
		return named != nil && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "database/sql/driver" && named.Obj().Name() == "Value"
	}
	return byteSlice(signature.Results().At(0).Type())
}

func byteSlice(t types.Type) bool {
	slice, ok := types.Unalias(t).(*types.Slice)
	return ok && types.Identical(slice.Elem(), types.Typ[types.Byte])
}

func emptyInterface(t types.Type) bool {
	value, ok := t.Underlying().(*types.Interface)
	return ok && value.NumMethods() == 0
}

// boundaries resolves by Go object identity, never by a caller's spelling of a
// method name. Native hooks also require a protected receiver and exact shape.
func (set *contractSet) boundariesFor(function *types.Func, policies policySet) []codecBoundary {
	if function == nil {
		return nil
	}
	if fact := set.boundaries[function.Origin()]; fact != nil {
		return fact.Boundaries
	}
	signature := function.Type().(*types.Signature)
	boundary := codecBoundary{ErrorResult: errorResult(signature)}
	path := ""
	if function.Pkg() != nil {
		path = function.Pkg().Path()
	}
	receiver := ""
	if signature.Recv() != nil {
		if named := domainType(signature.Recv().Type()); named != nil {
			receiver = named.Obj().Name()
		}
	}
	switch path {
	case "encoding/json":
		boundary.Codec = "json"
		switch {
		case receiver == "" && (function.Name() == "Marshal" || function.Name() == "MarshalIndent"), receiver == "Encoder" && function.Name() == "Encode":
			boundary.Encode = []int{0}
		case receiver == "" && function.Name() == "Unmarshal":
			boundary.Decode = []int{1}
		case receiver == "Decoder" && function.Name() == "Decode":
			boundary.Decode = []int{0}
		case receiver == "Unmarshaler" && function.Name() == "UnmarshalJSON":
			boundary.Decode = []int{-1}
		case receiver == "Marshaler" && function.Name() == "MarshalJSON":
			boundary.Encode = []int{-1}
		}
	case "encoding":
		boundary.Codec = "text"
		if receiver == "TextUnmarshaler" && function.Name() == "UnmarshalText" {
			boundary.Decode = []int{-1}
		}
		if receiver == "TextMarshaler" && function.Name() == "MarshalText" {
			boundary.Encode = []int{-1}
		}
	case "database/sql":
		boundary.Codec = "sql"
		switch {
		case (receiver == "Row" || receiver == "Rows") && function.Name() == "Scan":
			boundary.Decode = []int{0}
		case receiver == "Scanner" && function.Name() == "Scan":
			boundary.Decode = []int{-1}
		case receiver == "DB" || receiver == "Conn" || receiver == "Tx" || receiver == "Stmt":
			switch function.Name() {
			case "Exec", "Query", "QueryRow", "ExecContext", "QueryContext", "QueryRowContext":
				boundary.Encode = []int{signature.Params().Len() - 1}
			}
		case receiver == "" && function.Name() == "Named":
			boundary.Encode = []int{1}
		}
	case "database/sql/driver":
		boundary.Codec = "sql"
		if receiver == "Valuer" && function.Name() == "Value" {
			boundary.Encode = []int{-1}
		}
	}
	if len(boundary.Encode)+len(boundary.Decode) != 0 {
		return []codecBoundary{boundary}
	}
	if signature.Recv() == nil || policies.lookup(signature.Recv().Type()) == nil {
		return nil
	}
	for _, codec := range []string{"json", "text", "sql"} {
		if function.Name() == decoderMethod(codec) && validDecoder(signature, codec) {
			return []codecBoundary{{Codec: codec, Decode: []int{-1}, ErrorResult: 0}}
		}
		encoder := map[string]string{"json": "MarshalJSON", "text": "MarshalText", "sql": "Value"}[codec]
		if function.Name() == encoder && validEncoder(signature, codec) {
			return []codecBoundary{{Codec: codec, Encode: []int{-1}, ErrorResult: 1}}
		}
	}
	return nil
}

func callFunction(info *types.Info, call *ast.CallExpr) *types.Func {
	expression := call.Fun
	switch indexed := expression.(type) {
	case *ast.IndexExpr:
		expression = indexed.X
	case *ast.IndexListExpr:
		expression = indexed.X
	}
	var object types.Object
	switch expression := expression.(type) {
	case *ast.Ident:
		object = info.Uses[expression]
	case *ast.SelectorExpr:
		object = info.Uses[expression.Sel]
	}
	function, _ := object.(*types.Func)
	return function
}

func selectedReceiver(call *ast.CallExpr) ast.Expr {
	if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
		return selector.X
	}
	return nil
}

func serializationPosition(node ast.Node) token.Pos {
	if declaration, ok := node.(*ast.FuncDecl); ok {
		return declaration.Name.Pos()
	}
	return node.Pos()
}
