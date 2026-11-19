package domainconstruct

import (
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestContractInferenceBounds(t *testing.T) {
	check := &analysis.Analyzer{
		Name:     "checkcontractbounds",
		Doc:      "check inferred contracts for the test package",
		Requires: []*analysis.Analyzer{contractsAnalyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			contracts := pass.ResultOf[contractsAnalyzer].(*contractSet)
			for _, name := range []string{"Nested", "NestedGeneric", "Simple"} {
				function := pass.Pkg.Scope().Lookup(name).(*types.Func)
				contract := contracts.functions[function]
				switch name {
				case "Nested":
					if contract != nil && len(contract.Errors) != 0 {
						t.Errorf("nested branch inference established an error fact: %v", contract.Errors)
					}
				case "NestedGeneric":
					if contract != nil && len(contract.Sources) != 0 {
						t.Errorf("nested branch inference established a source fact: %v", contract.Sources)
					}
				case "Simple":
					if contract == nil || len(contract.Errors) != 1 || contract.Errors[0].Parameter != -1 {
						t.Errorf("simple function lost its non-nil error contract: %v", contract)
					}
				}
			}
			return nil, nil
		},
	}
	analysistest.Run(t, analysistest.TestData(), check, "contractbounds")
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
	payload := describePayload(shape, make(map[types.Type]bool), 0, &work)
	if nodes := count(payload); nodes > maxInferenceWork || payload.Kind != payloadStruct {
		t.Errorf("described %d nodes in payload kind %d with budget %d", nodes, payload.Kind, maxInferenceWork)
	}
}
