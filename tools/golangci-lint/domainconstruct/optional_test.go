package domainconstruct_test

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/domainconstruct"
)

func TestOptionalContracts(t *testing.T) {
	data := optionalTestData(t)
	for _, test := range []struct {
		name     string
		analyzer *analysis.Analyzer
		packages []string
	}{
		{"construction", domainconstruct.Analyzer, []string{"optionalconstruction"}},
		{"zero", domainconstruct.ZeroAnalyzer, []string{"optionalflow", "optionalclient"}},
		{"serialization", domainconstruct.SerializationAnalyzer, []string{"optionalserialization"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			analysistest.Run(t, data, test.analyzer, test.packages...)
		})
	}
}

// Analyze the real shared implementation, so changes to its layout or method
// contracts exercise the analyzer without a second implementation drifting.
func optionalTestData(t *testing.T) string {
	t.Helper()
	data := t.TempDir()
	if err := os.CopyFS(filepath.Join(data, "src"), os.DirFS(filepath.Join(analysistest.TestData(), "src"))); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../../server/pkg/optional/optional.go")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(data, "src/github.com/stolostron/ocm-fleet/server/pkg/optional")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "optional.go"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	return data
}
