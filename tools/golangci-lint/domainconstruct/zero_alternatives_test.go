package domainconstruct

import (
	"go/token"
	"reflect"
	"testing"
)

func TestZeroAlternativesStableResult(t *testing.T) {
	known := &zeroProblem{name: "ID", path: "Name", reason: reasonZeroValue,
		provenance: []zeroStep{{kind: "source", pos: token.Pos(1), detail: "first source"}}}
	possible := known.uncertain()
	otherType := &zeroProblem{name: "Ref", path: "Name", reason: reasonZeroValue}
	otherField := &zeroProblem{name: "ID", path: "Other", reason: reasonZeroValue}
	cases := []struct {
		name   string
		paths  []*zeroProblem
		stable bool
	}{
		{"valid", []*zeroProblem{nil}, false},
		{"known zero", []*zeroProblem{known}, false},
		{"same known zero", []*zeroProblem{known, known}, false},
		{"possible zero", []*zeroProblem{possible}, true},
		{"valid then zero", []*zeroProblem{nil, known}, true},
		{"zero then valid", []*zeroProblem{known, nil}, true},
		{"different types", []*zeroProblem{known, otherType}, true},
		{"different fields", []*zeroProblem{known, otherField}, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var alternatives zeroAlternatives
			var stable bool
			for _, problem := range test.paths {
				stable = alternatives.add(problem)
			}
			if stable != test.stable {
				t.Fatalf("result stable = %v, want %v", stable, test.stable)
			}
			if !stable {
				return
			}
			want := alternatives.problem()
			// Every possible suffix must preserve certainty, the selected problem,
			// its reason and its provenance once the result is stable.
			for _, suffix := range []*zeroProblem{nil, known, possible, otherType, otherField} {
				extended := alternatives
				extended.add(suffix)
				if got := extended.problem(); !reflect.DeepEqual(got, want) {
					t.Errorf("another path changed the stable result: got %#v, want %#v", got, want)
				}
			}
		})
	}
}
