package domainconstruct_test

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/domainconstruct"
)

func TestPresenceFlow(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), domainconstruct.ZeroAnalyzer,
		"presenceflow", "presenceclient", "presencepolicy")
	for _, result := range results {
		for _, finding := range result.Diagnostics {
			if !strings.Contains(finding.Message, "reason: unchecked-presence") {
				continue
			}
			if finding.Category != "domainzero.invalid-zero" ||
				!strings.Contains(finding.Message, "certainty: possible") {
				t.Errorf("unchecked presence lost its rule or certainty: %s", finding.Message)
			}
			if len(finding.Related) == 0 || !finding.Related[0].Pos.IsValid() {
				t.Errorf("unchecked presence has no source location: %s", finding.Message)
			}
		}
	}
}
