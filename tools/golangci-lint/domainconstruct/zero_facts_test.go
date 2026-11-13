package domainconstruct

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

func TestEdgeContextAssumptionsStayIndependent(t *testing.T) {
	function := buildFlowFunction(t, `package flow
func run(ready, branch, later bool, raw string) string {
  if !ready { return "" }
  value := raw
  if branch { value = "known" }
  if later { return value }
  return raw
}`)
	c := newFlowChecker(function)
	join := phiBlock(t, function)
	base := c.contextAt(join)
	later := function.Params[2]
	for _, state := range []truth{notZero, isZero, notZero} {
		context := base.clone()
		if !c.assume(&context, later, state) {
			t.Fatal("independent assumption is unexpectedly impossible")
		}
		for index := range join.Preds {
			edge := c.edgeContext(context, join, index)
			if !edge.possible || c.valueTruth(later, edge, make(map[ssa.Value]bool)) != state {
				t.Errorf("predecessor %d lost its independent assumption %v", index, state)
			}
		}
	}
	if actual := c.valueTruth(later, base, make(map[ssa.Value]bool)); actual != unknown {
		t.Errorf("branch assumptions escaped into the original context: %v", actual)
	}

	// A clone may be refined again after it has already been queried. Results
	// from its earlier assumptions must not be reused for the new facts.
	context := base.clone()
	_ = c.edgeContext(context, join, 0)
	c.assume(&context, later, notZero)
	edge := c.edgeContext(context, join, 0)
	if actual := c.valueTruth(later, edge, make(map[ssa.Value]bool)); actual != notZero {
		t.Errorf("refined context retained an earlier answer: %v", actual)
	}
}

func TestMemoryAcrossJoinsAllocations(t *testing.T) {
	query := memoryAcrossJoins(t, 8)
	allocations := testing.AllocsPerRun(3, func() {
		if problem := query(); problem == nil || !problem.possible {
			t.Fatal("opaque mutation must remain a possible invalid zero")
		}
	})
	// Leave room for allocator changes while catching enumeration of all 256
	// paths after the diagnostic has already become possible.
	if allocations > 2000 {
		t.Errorf("storage tracing across eight joins allocates %.0f times", allocations)
	}
}

func TestBooleanEqualityRefinement(t *testing.T) {
	function := buildFlowFunction(t, `package flow
func run(first, second bool) bool {
  same := first == second
  if same { return first }
  return second
}`)
	var comparison *ssa.BinOp
	for _, instruction := range function.Blocks[0].Instrs {
		if value, ok := instruction.(*ssa.BinOp); ok {
			comparison = value
		}
	}
	if comparison == nil {
		t.Fatal("fixture lost its boolean comparison")
	}
	for _, equal := range []truth{isZero, notZero} {
		for _, first := range []truth{isZero, notZero} {
			c := newFlowChecker(function)
			context := flowContext{facts: make(map[ssa.Value]truth), possible: true}
			c.assume(&context, comparison, equal)
			c.assume(&context, function.Params[0], first)
			want := first
			if equal == isZero {
				want = opposite(want)
			}
			if got := c.valueTruth(function.Params[1], context, make(map[ssa.Value]bool)); got != want {
				t.Errorf("equal=%v, first=%v: second=%v, want %v", equal, first, got, want)
			}
		}
	}
	// A comparison in the function does not establish equality until a branch
	// actually assumes its result.
	c := newFlowChecker(function)
	context := flowContext{facts: make(map[ssa.Value]truth), possible: true}
	c.assume(&context, function.Params[0], notZero)
	if got := c.valueTruth(function.Params[1], context, make(map[ssa.Value]bool)); got != unknown {
		t.Errorf("unassumed equality established second=%v", got)
	}
}

func BenchmarkBooleanEqualityRefinement(b *testing.B) {
	var source strings.Builder
	source.WriteString("package flow\nfunc run(first, second bool")
	for index := range 128 {
		fmt.Fprintf(&source, ", other%d bool", index)
	}
	source.WriteString(") bool { if first == second { return first }; return second }")
	function := buildFlowFunction(b, source.String())
	c := newFlowChecker(function)
	context := flowContext{facts: make(map[ssa.Value]truth), possible: true}
	for _, parameter := range function.Params[2:] {
		c.assume(&context, parameter, notZero)
	}
	b.ReportAllocs()
	for b.Loop() {
		c.refineBooleanEqualities(&context, function)
	}
}

func BenchmarkMemoryAcrossJoins(b *testing.B) {
	for _, joins := range []int{2, 4, 8, 32} {
		b.Run(fmt.Sprintf("joins=%d", joins), func(b *testing.B) {
			query := memoryAcrossJoins(b, joins)
			b.ReportAllocs()
			for b.Loop() {
				if problem := query(); problem == nil || !problem.possible {
					b.Fatal("opaque mutation must remain a possible invalid zero")
				}
			}
		})
	}
}

func memoryAcrossJoins(t testing.TB, joins int) func() *zeroProblem {
	t.Helper()
	var source strings.Builder
	source.WriteString(`package flow
type ID string
func run(keep func(*string), mark func(int), flags []bool, valid ID) ID {
  raw := string(valid)
  keep(&raw)
`)
	for index := range joins {
		fmt.Fprintf(&source, "if flags[%d] { mark(1) } else { mark(2) }\n", index)
	}
	source.WriteString("return ID(raw)\n}")
	function := buildFlowFunction(t, source.String())
	storage, result := returnedStorage(t, function, "raw")
	typ := function.Signature.Results().At(0).Type()
	object := typ.(*types.Named).Obj()
	point := instructionPoint(result)
	return func() *zeroProblem {
		c := newFlowChecker(function)
		c.policies = policySet{object: {name: "flow.ID", typ: typ, fact: &constructionFact{}}}
		context := c.contextAt(point.block)
		return c.memoryProblem(address{root: storage}, typ, point, context, make(map[valueVisit]bool), make(map[memoryVisit]bool))
	}
}

func buildFlowFunction(t testing.TB, source string) *ssa.Function {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "flow.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ssautil.BuildPackage(&types.Config{}, fset, types.NewPackage("flow", "flow"), []*ast.File{file}, ssa.SanityCheckFunctions)
	if err != nil {
		t.Fatal(err)
	}
	return pkg.Func("run")
}

func newFlowChecker(function *ssa.Function) *zeroChecker {
	c := &zeroChecker{
		pass:        &analysis.Pass{Fset: function.Prog.Fset, Pkg: function.Pkg.Pkg},
		contracts:   &contractSet{},
		contexts:    make(map[*ssa.BasicBlock]flowContext),
		interesting: make(map[types.Type]bool),
		locating:    make(map[ssa.Value]bool),
		expressions: make(map[token.Pos]sourceExpression),
	}
	c.indexStores([]*ssa.Function{function})
	return c
}

func phiBlock(t testing.TB, function *ssa.Function) *ssa.BasicBlock {
	t.Helper()
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			if _, ok := instruction.(*ssa.Phi); ok {
				return block
			}
		}
	}
	t.Fatal("fixture has no merge value")
	return nil
}
