package domainconstruct_test

import (
	"go/ast"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/domainconstruct"
)

func TestZeroFlow(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), domainconstruct.ZeroAnalyzer,
		"zeroflow", "zeroclient", "zeromutator")
	expectations := map[string]struct {
		source string
		steps  []string
	}{
		"MutationThroughVariadic":       {steps: []string{"source", "mutation", "conversion", "assignment", "use"}},
		"PrimitiveFieldMayBeZero":       {source: "input.Raw", steps: []string{"source", "conversion", "use"}},
		"NestedPrimitiveFieldMayBeZero": {source: "input.Metadata.Name", steps: []string{"source", "conversion", "use"}},
		"RawReceiverFieldMayBeZero":     {source: "s.Name", steps: []string{"source", "conversion", "use"}},
	}
	for _, result := range results {
		checkUsePositions(t, result)
		for _, finding := range result.Diagnostics {
			if finding.Category != "domainzero.invalid-zero" &&
				!strings.Contains(finding.Message, "| rule: "+finding.Category+"; certainty: known;") {
				t.Errorf("convention and policy violations must have known certainty: %s", finding.Message)
			}
		}
		for _, file := range result.Pass.Files {
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok {
					continue
				}
				want, ok := expectations[function.Name.Name]
				if !ok {
					continue
				}
				found := false
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Pos < function.Pos() || diagnostic.Pos > function.End() {
						continue
					}
					found = true
					if function.Name.Name == "RawReceiverFieldMayBeZero" {
						for _, text := range []string{
							"return may produce an empty zeroflow.ID; empty values are invalid",
							"| rule: domainzero.invalid-zero; certainty: possible; reason: input-may-be-zero",
							`| operation: return; value: "s.Name"; type: "zeroflow.ID"; field-path: ""`,
						} {
							if !strings.Contains(diagnostic.Message, text) {
								t.Errorf("receiver field finding missing %q: %s", text, diagnostic.Message)
							}
						}
					}
					if diagnostic.Category != "domainzero.invalid-zero" {
						t.Errorf("%s category = %q, want domainzero.invalid-zero", function.Name.Name, diagnostic.Category)
					}
					if !strings.Contains(diagnostic.Message, "certainty: possible") {
						t.Errorf("%s must retain possible certainty: %s", function.Name.Name, diagnostic.Message)
					}
					for _, kind := range want.steps {
						present := false
						for _, related := range diagnostic.Related {
							if strings.HasPrefix(related.Message, kind+":") && related.Pos.IsValid() {
								present = true
							}
						}
						if !present {
							t.Errorf("%s diagnostic has no structured %s step", function.Name.Name, kind)
						}
					}
					if want.source != "" {
						present := false
						for _, related := range diagnostic.Related {
							if strings.HasPrefix(related.Message, "source: "+want.source+" ") &&
								strings.Contains(related.Message, " as a plain string") {
								present = true
							}
						}
						if !present {
							t.Errorf("%s diagnostic has no source naming %s", function.Name.Name, want.source)
						}
					}
				}
				if !found {
					t.Errorf("%s has no zero diagnostic", function.Name.Name)
				}
			}
		}
	}
}
