package domainconstruct_test

import (
	"go/ast"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// Source expressions and storage descriptions have different jobs: an anonymous
// literal has members, but no variable named "complit" or "nil".
func checkValueNames(t *testing.T, result *analysistest.Result) {
	t.Helper()
	type expectation struct {
		value string
		step  string
	}
	wants := map[string]expectation{
		"NameGenericLiteral": {
			"nameExecution[nameView]{val: nameView{}}",
			"assignment: assign to composite literal at path val.Deployment.ID",
		},
		"NamePointerLiteral": {
			"Request{}", "source: storage composite literal at path ID starts at zero",
		},
		"NameExplicitZeroField": {
			`""`, "assignment: assign to composite literal at path ID",
		},
		"NameArrayLiteral": {
			"[1]Request{}", "source: collection contains zero-initialized elements",
		},
		"NameSliceLiteral": {
			"[]Request{{}}", "assignment: assign collection element [0]",
		},
		"NameNewStorage": {
			"new(Request).ID", "source: storage new(Request).ID starts at zero",
		},
		"NameNamedStorage": {
			"record.ID", "source: storage record.ID starts at zero",
		},
		"NameCompilerLikeVariable": {
			"complit.ID", "source: storage complit.ID starts at zero",
		},
		// Optimized aggregate constants have no source position. Leaving Value
		// empty is more accurate than inventing a nil expression or a variable.
		"NameZeroStruct": {"", ""},
		"NameZeroArray":  {"", ""},
		"NameZeroRef":    {"", ""},
		"NameNonNilOption": {
			"Request{}", "source: storage composite literal at path ID starts at zero",
		},
		"NameNamedProjection": {
			"input.Name", "source: input.Name enters this function as a plain string",
		},
		"NameIndirectMutation": {
			"raw", "mutation: factory() may modify raw; nonzero initialization afterward is unproven",
		},
		"NameTupleMutation": {
			"raw", "mutation: mutate may modify raw; nonzero initialization afterward is unproven",
		},
		"NameAnonymousMutation": {
			"raw", "mutation: anonymous function may modify raw; nonzero initialization afterward is unproven",
		},
		"NameNamedClosureMutation": {
			"raw", "mutation: update may modify raw; nonzero initialization afterward is unproven",
		},
		"NameVariadicLiteral": {"", ""},
	}
	for _, file := range result.Pass.Files {
		if !strings.HasSuffix(result.Pass.Fset.Position(file.Pos()).Filename, "/diagnostic_names.go") {
			continue
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			want, ok := wants[function.Name.Name]
			if !ok {
				continue
			}
			t.Run(function.Name.Name, func(t *testing.T) {
				found := 0
				for _, finding := range result.Diagnostics {
					if finding.Pos < function.Pos() || finding.Pos > function.End() {
						continue
					}
					found++
					if !strings.Contains(finding.Message, "; value: "+strconv.Quote(want.value)+"; type:") {
						t.Errorf("want source value %q: %s", want.value, finding.Message)
					}
					if want.step != "" {
						present := false
						for _, related := range finding.Related {
							present = present || related.Message == want.step
						}
						if !present {
							t.Errorf("want provenance %q: %v", want.step, finding.Related)
						}
					}
				}
				if found != 1 {
					t.Errorf("got %d diagnostics, want one", found)
				}
			})
		}
	}
}
