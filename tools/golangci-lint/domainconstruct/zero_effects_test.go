package domainconstruct

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestMutationReachabilityAcrossJoins(t *testing.T) {
	c, raw, invoke := mutationAcrossJoins(t)
	if kind := c.callMutation(&invoke.Call, address{root: raw}, instructionPoint(invoke)); kind != mutationReadOnly {
		t.Errorf("invocation of unrelated destinations has mutation kind %v", kind)
	}
}

func TestRepeatedMutationReachabilityAllocations(t *testing.T) {
	c, raw, invoke := mutationAcrossJoins(t)
	point := instructionPoint(invoke)
	allocations := testing.AllocsPerRun(3, func() {
		if kind := c.callMutation(&invoke.Call, address{root: raw}, point); kind != mutationReadOnly {
			t.Fatalf("invocation of unrelated destinations has mutation kind %v", kind)
		}
	})
	if allocations > 5 {
		t.Errorf("repeated reachability query allocates %.0f times", allocations)
	}
}

func TestMutationTargetsStayIndependent(t *testing.T) {
	function := buildFlowFunction(t, `package flow
type Record struct { First, Second string }
func run(mutate func(...any), keep func(*Record, *[2]string)) {
  var record Record
  var elements [2]string
  keep(&record, &elements)
  mutate(&record.First)
  mutate(&elements[1])
}`)
	var record, elements *ssa.Alloc
	var calls []*ssa.Call
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			if allocation, ok := instruction.(*ssa.Alloc); ok {
				switch allocation.Comment {
				case "record":
					record = allocation
				case "elements":
					elements = allocation
				}
			}
			if call, ok := instruction.(*ssa.Call); ok && call.Call.Value == function.Params[0] {
				calls = append(calls, call)
			}
		}
	}
	if record == nil || elements == nil || len(calls) != 2 {
		t.Fatal("fixture lost its storage or mutations")
	}
	c := newFlowChecker(function)
	for _, index := range []int{0, 1, 0} {
		want := mutationReadOnly
		if index == 0 {
			want = mutationUnknown
		}
		if got := c.callMutation(&calls[0].Call, address{root: record, path: []int{index}}, instructionPoint(calls[0])); got != want {
			t.Errorf("field %d mutation=%v, want %v", index, got, want)
		}
	}
	for _, index := range []int64{0, 1, 0} {
		want := mutationReadOnly
		if index == 1 {
			want = mutationUnknown
		}
		view := collectionView{storage: address{root: elements}, start: index, end: index + 1}
		if got := c.collectionMutation(&calls[1].Call, view, instructionPoint(calls[1])); got != want {
			t.Errorf("element %d mutation=%v, want %v", index, got, want)
		}
	}
}

func BenchmarkMutationReachabilityAcrossJoins(b *testing.B) {
	c, raw, invoke := mutationAcrossJoins(b)
	point := instructionPoint(invoke)
	b.ReportAllocs()
	for b.Loop() {
		if kind := c.callMutation(&invoke.Call, address{root: raw}, point); kind != mutationReadOnly {
			b.Fatalf("invocation of unrelated destinations has mutation kind %v", kind)
		}
	}
}

func mutationAcrossJoins(t testing.TB) (*zeroChecker, *ssa.Alloc, *ssa.Call) {
	t.Helper()
	var source strings.Builder
	source.WriteString(`package flow
func run(keep func(*string), invoke func(...any), mark func(int), flags []bool) {
  var raw, other string
  keep(&raw)
  destinations := []any{&other}
`)
	// Side effects preserve the joins in SSA. A path-by-path traversal would
	// inspect billions of paths even though each block has the same storage.
	for index := range 32 {
		fmt.Fprintf(&source, "if flags[%d] { mark(1) } else { mark(2) }\n", index)
	}
	source.WriteString("invoke(destinations...)\n}")
	function := buildFlowFunction(t, source.String())
	var raw *ssa.Alloc
	var invoke *ssa.Call
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			if allocation, ok := instruction.(*ssa.Alloc); ok && allocation.Comment == "raw" {
				raw = allocation
			}
			if call, ok := instruction.(*ssa.Call); ok && call.Call.Value.Name() == "invoke" {
				invoke = call
			}
		}
	}
	if raw == nil || invoke == nil {
		t.Fatal("fixture did not retain output storage and its invocation")
	}
	return newFlowChecker(function), raw, invoke
}
