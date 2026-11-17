package domainconstruct

import (
	"go/ast"
	"go/types"
	"strings"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/diagnostic"
)

// presenceFact declares that result zero is usable only when result one is
// true and the optional error result is nil. It describes an API contract;
// a boolean's name or a function's body does not grant that contract.
type presenceFact struct{}

func (*presenceFact) AFact() {}

func (b *contractBuilder) collectPresences() {
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
			count, arguments := 0, false
			for _, comment := range doc.List {
				fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(comment.Text, "//")))
				if len(fields) != 0 && fields[0] == "domain:presence" {
					count++
					arguments = arguments || len(fields) != 1
				}
			}
			if count == 0 {
				return true
			}
			message := ""
			switch {
			case count != 1 || arguments:
				message = "use domain:presence once, without arguments"
			case !presenceSignature(function.Type().(*types.Signature)):
				message = "domain:presence requires (T, bool) or (T, bool, error) results"
			}
			if message != "" {
				b.set.presenceDiagnostics = append(b.set.presenceDiagnostics, (diagnostic.Finding{
					Rule: "domainzero.invalid-presence-policy", Certainty: diagnostic.Known,
					Operation: "declaration", Value: function.Name(), Type: typeIdentity(function.Type()),
					Reason: "invalid-presence", Pos: name.Pos(), Message: message,
				}).Diagnostic())
				return true
			}
			b.set.presences[function.Origin()] = true
			b.pass.ExportObjectFact(function.Origin(), new(presenceFact))
			return true
		})
	}
}

func presenceSignature(signature *types.Signature) bool {
	results := signature.Results()
	return (results.Len() == 2 || results.Len() == 3 && isErrorType(results.At(2).Type())) &&
		types.Identical(results.At(1).Type(), types.Typ[types.Bool]) &&
		!isErrorType(results.At(0).Type())
}

func (set *contractSet) hasPresence(function *types.Func) bool {
	return function != nil && set.presences[function.Origin()]
}
