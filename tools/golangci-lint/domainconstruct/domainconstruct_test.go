package domainconstruct_test

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/domainconstruct"
)

func TestConstruction(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), domainconstruct.Analyzer,
		"values", "client", "badpolicy")
	for _, result := range results {
		for _, finding := range result.Diagnostics {
			if !strings.Contains(finding.Message, "| rule: "+finding.Category+"; certainty: known;") {
				t.Errorf("construction and policy violations must have known certainty: %s", finding.Message)
			}
		}
	}
}
