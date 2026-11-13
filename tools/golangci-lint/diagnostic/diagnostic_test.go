package diagnostic_test

import (
	"go/token"
	"strings"
	"testing"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/diagnostic"
)

func TestFindingKeepsMetadataAndNativeLocations(t *testing.T) {
	fset := token.NewFileSet()
	source := fset.AddFile("source.go", -1, 100)
	use := fset.AddFile("use.go", -1, 100)
	finding := diagnostic.Finding{
		Rule: "domainzero.invalid-zero", Certainty: diagnostic.Possible,
		Operation: "return", Value: "s.Name", Type: "example.com/domain.ID",
		FieldPath: "Reports[0].ID", Reason: "input-may-be-zero",
		Pos: use.Pos(20), Message: "return may contain an empty domain.ID",
		Provenance: []diagnostic.Step{
			{Kind: "source", Pos: source.Pos(7), Message: "s.Name enters as a plain string"},
			{Kind: "use", Pos: use.Pos(20), Message: "return"},
		},
	}
	native := finding.Diagnostic()
	if native.Category != finding.Rule || native.Pos != finding.Pos {
		t.Fatalf("lost primary rule or location: %+v", native)
	}
	for _, text := range []string{
		"| rule: domainzero.invalid-zero; certainty: possible; reason: input-may-be-zero",
		`| operation: return; value: "s.Name"; type: "example.com/domain.ID"; field-path: "Reports[0].ID"`,
	} {
		if !strings.Contains(native.Message, text) {
			t.Errorf("missing metadata %q: %s", text, native.Message)
		}
	}
	if len(native.Related) != 2 || native.Related[0].Pos != source.Pos(7) {
		t.Fatalf("lost native cross-file provenance: %+v", native.Related)
	}
	adapted := diagnostic.WithRelatedLocations(fset, native)
	if len(adapted.Related) != 0 || !strings.Contains(adapted.Message, `| source: s.Name enters as a plain string ("source.go", line 1, column 8)`) {
		t.Fatalf("lost grouped cross-file provenance: %+v", adapted)
	}
	if len(native.Related) != 2 {
		t.Fatal("adapting a diagnostic mutated its native related locations")
	}
}

func TestMetadataEscapesExpressionsAndProvenance(t *testing.T) {
	fset := token.NewFileSet()
	file := fset.AddFile(`a | rule: "source".go`, -1, 100)
	finding := diagnostic.Finding{
		Rule: "domainconstruct.constructor-bypass", Certainty: diagnostic.Known,
		Operation: "construction", Value: "ID(\"x; | rule: y\")\n", Type: "example.com/domain.ID",
		Reason: "constructor-required", Pos: file.Pos(0), Message: "construct domain.ID through NewID",
		Provenance: []diagnostic.Step{{Kind: "conversion", Pos: file.Pos(0), Message: "a | b\nuses \\ representation"}},
	}
	adapted := diagnostic.WithRelatedLocations(fset, finding.Diagnostic())
	for _, text := range []string{
		`certainty: known`,
		`value: "ID(\"x; \u007c rule: y\")\n"`,
		`| conversion: a \| b\nuses \\ representation ("a \u007c rule: \"source\".go", line 1, column 1)`,
	} {
		if !strings.Contains(adapted.Message, text) {
			t.Errorf("missing escaped text %q: %s", text, adapted.Message)
		}
	}
}
