package domainconstruct

import (
	"fmt"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestDescribePayloadBoundsFieldsAtMaximumDepth(t *testing.T) {
	fields := make([]*types.Var, maxInferenceWork+1)
	for index := range fields {
		fields[index] = types.NewField(token.NoPos, nil, fmt.Sprintf("Field%d", index), types.Typ[types.String], false)
	}
	var shape types.Type = types.NewStruct(fields, nil)
	for range 8 {
		shape = types.NewStruct([]*types.Var{types.NewField(token.NoPos, nil, "Nested", shape, false)}, nil)
	}
	work := workBudget(maxInferenceWork)
	payload := describePayload(shape, make(map[types.Type]bool), 0, false, &work)
	for range 8 {
		if len(payload.Fields) != 1 {
			t.Fatalf("lost the bounded path to the wide field")
		}
		payload = payload.Fields[0].Type
	}
	if len(payload.Fields) != 0 {
		t.Errorf("emitted %d fields after reaching the depth limit", len(payload.Fields))
	}
}

func TestContractInferenceBounds(t *testing.T) {
	check := &analysis.Analyzer{
		Name:     "checkcontractbounds",
		Doc:      "check inferred contracts for the test package",
		Requires: []*analysis.Analyzer{contractsAnalyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			contracts := pass.ResultOf[contractsAnalyzer].(*contractSet)
			names := []string{
				"Nested", "NestedGeneric", "Simple", "LargeComposition", "SmallComposition",
				"AtSourceDepth", "ChoiceBeyondSourceDepth", "RecoveredSlice", "PlainSlice",
				"ResetPromotedSource", "KeepPromotedSource",
			}
			for _, name := range names {
				function := pass.Pkg.Scope().Lookup(name).(*types.Func)
				contract := contracts.functions[function]
				switch name {
				case "Nested":
					if contract != nil && len(contract.Errors) != 0 {
						t.Errorf("nested branch inference established an error fact: %v", contract.Errors)
					}
				case "NestedGeneric", "LargeComposition", "ChoiceBeyondSourceDepth":
					if contract != nil && len(contract.Sources) != 0 {
						t.Errorf("%s exported a source beyond the work limit", name)
					}
				case "SmallComposition", "AtSourceDepth":
					if contract == nil || len(contract.Sources) != 1 || contract.Sources[0].Value.Kind != sourceFields {
						t.Errorf("%s lost its field sources: %v", name, contract)
					}
				case "Simple":
					if contract == nil || len(contract.Errors) != 1 || contract.Errors[0].Parameter != -1 {
						t.Errorf("simple function lost its non-nil error contract: %v", contract)
					}
				case "RecoveredSlice":
					if contract != nil && len(contract.Lengths) != 0 {
						t.Errorf("recovery can bypass the inferred length: %v", contract.Lengths)
					}
				case "PlainSlice":
					if contract == nil || len(contract.Lengths) != 1 || contract.Lengths[0].Length.Parameter != -1 || contract.Lengths[0].Length.Constant != 1 {
						t.Errorf("plain allocation lost its length fact: %v", contract)
					}
				case "ResetPromotedSource":
					if contract != nil && len(contract.Sources) != 0 {
						t.Errorf("promoted pointer method left stale sources: %v", contract.Sources)
					}
				case "KeepPromotedSource":
					if contract == nil || len(contract.Sources) != 1 || contract.Sources[0].Value.Kind != sourceParameter {
						t.Errorf("unchanged embedded field lost its source: %v", contract)
					}
				}
			}
			return nil, nil
		},
	}
	analysistest.Run(t, analysistest.TestData(), check, "contractbounds")
}

func TestContractRoundLimitRetainsIndependentFacts(t *testing.T) {
	check := &analysis.Analyzer{
		Name:     "checkcontractchain",
		Doc:      "check retained and unfinished contracts",
		Requires: []*analysis.Analyzer{contractsAnalyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			contracts := pass.ResultOf[contractsAnalyzer].(*contractSet)
			for _, name := range []string{"Independent", "AlsoIndependent", "First", "DependencyOnUnfinished"} {
				function := pass.Pkg.Scope().Lookup(name).(*types.Func)
				fact := contracts.functions[function]
				want, count := 0, 0
				if name == "Independent" || name == "AlsoIndependent" {
					want = 1
				}
				if fact != nil {
					count = len(fact.Sources)
				}
				if count != want {
					t.Errorf("%s retained %d source facts, want %d", name, count, want)
				}
			}
			sentinel := pass.Pkg.Scope().Lookup("IndependentError").(*types.Var)
			if !contracts.sentinels[sentinel] {
				t.Error("round limit discarded independent sentinel")
			}
			return nil, nil
		},
	}
	analysistest.Run(t, analysistest.TestData(), check, "contractchain")
}

func TestProjectSourceBoundsRecursivePaths(t *testing.T) {
	source := valueSource{Kind: sourceParameter}
	for range 100 {
		source = projectSource(source, []int{1})
	}
	if source.Kind != sourceUnknown {
		t.Errorf("repeated projection retained %d fields", len(source.Path))
	}
}

func TestDescribePayloadBoundsSharedTypeShapes(t *testing.T) {
	var shape types.Type = types.Typ[types.String]
	for range 8 {
		fields := make([]*types.Var, 0, 4)
		for _, name := range []string{"First", "Second", "Third", "Fourth"} {
			fields = append(fields, types.NewField(token.NoPos, nil, name, shape, false))
		}
		shape = types.NewStruct(fields, nil)
	}
	var count func(payloadType) int
	count = func(payload payloadType) int {
		nodes := 1
		for _, field := range payload.Fields {
			nodes += count(field.Type)
		}
		return nodes
	}
	work := workBudget(maxInferenceWork)
	payload := describePayload(shape, make(map[types.Type]bool), 0, false, &work)
	if nodes := count(payload); nodes > maxInferenceWork || payload.Kind != payloadStruct {
		t.Errorf("described %d nodes in payload kind %d with budget %d", nodes, payload.Kind, maxInferenceWork)
	}
}
