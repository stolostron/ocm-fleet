package domainconstruct

import (
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestSliceChoiceRequiresAgreementOnLengthBase(t *testing.T) {
	function := buildFlowFunction(t, `package flow
func choose(first, second []string) []string { return first }
func run(n int) {
	base := make([]string, 2)
	_ = choose(base[1:n], base[:n])
}`)
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			call, ok := instruction.(*ssa.Call)
			if !ok {
				continue
			}
			choice := valueSource{Kind: sourceChoice, Values: []valueSource{
				{Kind: sourceSlice, Length: sourceLength{Constant: -1, Parameter: 0, FromLen: true}},
				{Kind: sourceSlice, Length: sourceLength{Constant: -1, Parameter: 1, FromLen: true}},
			}}
			checker := newFlowChecker(function)
			slice := call.Type().(*types.Slice)
			first, firstKnown := checker.sliceSourceView(choice.Values[0], call, call, slice)
			second, secondKnown := checker.sliceSourceView(choice.Values[1], call, call, slice)
			if !firstKnown || !secondKnown || !sameLength(first.length, second.length) || first.lengthBase == second.lengthBase {
				t.Fatalf("expected the same length expression at different bases, got %+v and %+v", first, second)
			}
			view, known := checker.sliceSourceView(choice, call, call, slice)
			if !known || view.end >= 0 || view.length != nil {
				t.Errorf("merged view claims an exact length bound: %+v", view)
			}
			return
		}
	}
	t.Fatal("missing choice call")
}
