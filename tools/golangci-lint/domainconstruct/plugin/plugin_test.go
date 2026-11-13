package plugin

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/domainconstruct"
)

func TestRelatedInformationStaysWithFinding(t *testing.T) {
	directory, err := filepath.Abs("../testdata")
	if err != nil {
		t.Fatal(err)
	}
	analyzers, err := (plugin{domainconstruct.ZeroAnalyzer}).BuildAnalyzers()
	if err != nil {
		t.Fatal(err)
	}
	results := analysistest.Run(t, directory, analyzers[0], "zeroflow")
	found := false
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			if !strings.Contains(diagnostic.Message, "\n  | mutation: ") {
				continue
			}
			found = true
			if len(diagnostic.Related) != 0 {
				t.Error("golangci-lint would flatten related information into extra findings")
			}
			for _, kind := range []string{"source", "mutation", "use"} {
				if !strings.Contains(diagnostic.Message, "\n  | "+kind+": ") {
					t.Errorf("rendered diagnostic has no %s provenance: %s", kind, diagnostic.Message)
				}
			}
		}
	}
	if !found {
		t.Fatal("no mutation finding was exercised")
	}
}
