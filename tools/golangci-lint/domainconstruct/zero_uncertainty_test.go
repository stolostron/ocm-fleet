package domainconstruct

import (
	"go/token"
	"reflect"
	"testing"
)

func TestZeroUncertaintyPreservesSourceReason(t *testing.T) {
	for _, reason := range []zeroReason{
		reasonZeroValue,
		reasonZeroStorage,
		reasonInputMayBeZero,
		reasonCleared,
		reasonUncheckedError,
	} {
		for _, test := range []struct {
			name     string
			possible bool
		}{{"known", false}, {"possible", true}} {
			t.Run(string(reason)+"/"+test.name, func(t *testing.T) {
				source := &zeroProblem{
					name: "ID", path: "Name", reason: reason, possible: test.possible,
					provenance: []zeroStep{{kind: "source", pos: token.Pos(1), detail: "original zero source"}},
				}
				original := *source
				result := source.uncertain()
				if !result.possible || result.reason != reason {
					t.Errorf("uncertainty changed source %s: got reason %s, possible %v",
						reason, result.reason, result.possible)
				}
				if !reflect.DeepEqual(result.provenance, source.provenance) {
					t.Error("uncertainty changed source provenance")
				}
				if !reflect.DeepEqual(*source, original) {
					t.Error("uncertainty changed the original problem")
				}
				if again := result.uncertain(); !reflect.DeepEqual(again, result) {
					t.Error("repeated uncertainty changed the source evidence")
				}
			})
		}
	}
}

func TestZeroAlternativesPreserveSourceReason(t *testing.T) {
	source := &zeroProblem{name: "ID", reason: reasonZeroStorage}
	for _, paths := range [][]*zeroProblem{{nil, source}, {source, nil}} {
		var alternatives zeroAlternatives
		for _, problem := range paths {
			alternatives.add(problem)
		}
		result := alternatives.problem()
		if result == nil || result.reason != reasonZeroStorage || !result.possible {
			t.Errorf("a valid alternative replaced the zero source: %#v", result)
		}
	}
}

func TestUnknownMutationCannotCreateZeroConcern(t *testing.T) {
	var valid *zeroProblem
	if result := valid.afterUnknownMutation(token.Pos(1), "mutate", "value"); result != nil {
		t.Errorf("mutation created a zero concern for a valid value: %#v", result)
	}
}
