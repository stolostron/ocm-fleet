package domainconstruct

import (
	"fmt"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestUntouchedFieldsKeepTheirZeroContract(t *testing.T) {
	function := buildFlowFunction(t, `package flow
type ID string
func (ID) Method() {}
type Record struct {
  Required ID
  Optional any
  Callback func()
  Parent *Record
  Explicit any
  Nested struct { Payload any }
  Bound func()
}
func run(keep func(*Record)) Record {
  record := Record{Explicit: ID(""), Bound: ID("").Method}
  record.Nested.Payload = ID("")
  copied := record
  keep(&copied)
  return copied
}`)
	c := newFlowChecker(function)
	object := function.Pkg.Pkg.Scope().Lookup("ID").(*types.TypeName)
	c.policies = policySet{object: {name: "flow.ID", typ: object.Type(), fact: &constructionFact{Constructors: []string{"NewID"}}}}
	storage, result := returnedStorage(t, function, "copied")
	structure := function.Signature.Results().At(0).Type().Underlying().(*types.Struct)
	point := instructionPoint(result)
	context := c.contextAt(point.block)
	for index, wantProblem := range []bool{true, false, false, false, true, true, true} {
		field := structure.Field(index)
		t.Run(field.Name(), func(t *testing.T) {
			location := address{root: storage, path: []int{index}}
			problem := c.memoryProblem(location, field.Type(), point, context, make(map[valueVisit]bool), make(map[memoryVisit]bool))
			if (problem != nil) != wantProblem {
				t.Errorf("field problem = %#v, want problem %v", problem, wantProblem)
			}
		})
	}
}

func TestZeroStorageProofRemainsConservative(t *testing.T) {
	for _, test := range []struct {
		name         string
		body         string
		storage      string
		wantShortcut bool
		wantProblem  bool
	}{
		{"untouched", `var record Record; keep(&record); return record`, "record", true, false},
		{"sibling assignment", `record := Record{Label: "known"}; keep(&record); return record`, "record", true, false},
		{"boxed zero", `record := Record{Payload: ID("")}; keep(&record); return record`, "record", false, true},
		{"overwritten payload", `record := Record{Payload: ID("")}; record.Payload = nil; keep(&record); return record`, "record", false, false},
		{"later write to source", `record := Record{}; keep(&record); copied := record; record.Payload = ID(""); keep(&copied); return copied`, "copied", false, false},
		{"copy cycle", `var record, other Record; keep(&record); keep(&other); record = other; other = record; return record`, "record", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			function := buildFlowFunction(t, `package flow
type ID string
type Record struct { Label string; Payload any }
func run(keep func(*Record)) Record {`+test.body+`}`)
			c := newFlowChecker(function)
			object := function.Pkg.Pkg.Scope().Lookup("ID").(*types.TypeName)
			c.policies = policySet{object: {name: "flow.ID", typ: object.Type(), fact: &constructionFact{Constructors: []string{"NewID"}}}}
			storage, result := returnedStorage(t, function, test.storage)
			typ := function.Signature.Results().At(0).Type().Underlying().(*types.Struct).Field(1).Type()
			location := address{root: storage, path: []int{1}}
			if got := c.validZeroStorage(location, typ); got != test.wantShortcut {
				t.Errorf("storage shortcut = %v, want %v", got, test.wantShortcut)
			}
			point := instructionPoint(result)
			problem := c.memoryProblem(location, typ, point, c.contextAt(point.block), make(map[valueVisit]bool), make(map[memoryVisit]bool))
			if (problem != nil) != test.wantProblem {
				t.Errorf("problem = %#v, want problem %v", problem, test.wantProblem)
			}
		})
	}
}

func TestUntouchedFieldTracingAllocations(t *testing.T) {
	for _, factory := range []bool{false, true} {
		t.Run(fmt.Sprintf("factory=%v", factory), func(t *testing.T) {
			query := untouchedFieldQuery(t, 8, factory)
			allocations := testing.AllocsPerRun(3, func() {
				if problem := query(); problem != nil {
					t.Fatalf("untouched optional field has a problem: %#v", problem)
				}
			})
			if allocations > 2000 {
				t.Errorf("tracing an untouched optional field allocates %.0f times", allocations)
			}
		})
	}
}

func TestStoragePathOverlap(t *testing.T) {
	for _, test := range []struct {
		name          string
		first, second []int
		want          bool
	}{
		{"whole value", nil, []int{1, 2}, true},
		{"parent", []int{1}, []int{1, 2}, true},
		{"same field", []int{1, 2}, []int{1, 2}, true},
		{"sibling", []int{1, 2}, []int{1, 3}, false},
		{"dynamic index", []int{1, -1}, []int{1, 2, 3}, true},
		{"dynamic sibling", []int{1, -1, 2}, []int{1, 3, 4}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := storagePathsOverlap(test.first, test.second); got != test.want {
				t.Errorf("overlap = %v, want %v", got, test.want)
			}
			if got := storagePathsOverlap(test.second, test.first); got != test.want {
				t.Errorf("reverse overlap = %v, want %v", got, test.want)
			}
		})
	}
}

func BenchmarkUntouchedFieldAcrossJoins(b *testing.B) {
	for _, factory := range []bool{false, true} {
		for _, joins := range []int{2, 4, 8, 32} {
			b.Run(fmt.Sprintf("factory=%v/joins=%d", factory, joins), func(b *testing.B) {
				query := untouchedFieldQuery(b, joins, factory)
				b.ReportAllocs()
				for b.Loop() {
					if problem := query(); problem != nil {
						b.Fatalf("untouched optional field has a problem: %#v", problem)
					}
				}
			})
		}
	}
}

func untouchedFieldQuery(t testing.TB, joins int, factory bool) func() *zeroProblem {
	t.Helper()
	var source strings.Builder
	source.WriteString(`package flow
type Record struct { Name string; Optional any }
func run(factory func() *Record, keep func(*Record), mark func(int), flags []bool) Record {
`)
	if factory {
		source.WriteString("record := factory()\nkeep(record)\n")
	} else {
		source.WriteString("record := Record{Name: \"known\"}\nkeep(&record)\n")
	}
	for index := range joins {
		fmt.Fprintf(&source, "if flags[%d] { mark(1) } else { mark(2) }\n", index)
	}
	if factory {
		source.WriteString("return *record\n}")
	} else {
		source.WriteString("return record\n}")
	}
	function := buildFlowFunction(t, source.String())
	var result *ssa.Return
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			if statement, ok := instruction.(*ssa.Return); ok {
				result = statement
			}
		}
	}
	if result == nil {
		t.Fatal("fixture lost its return")
	}
	load, ok := result.Results[0].(*ssa.UnOp)
	if !ok {
		t.Fatalf("returned value is %T, want a storage read", result.Results[0])
	}
	storage := load.X
	typ := function.Signature.Results().At(0).Type().Underlying().(*types.Struct).Field(1).Type()
	point := instructionPoint(result)
	return func() *zeroProblem {
		c := newFlowChecker(function)
		context := c.contextAt(point.block)
		return c.memoryProblem(address{root: storage, path: []int{1}}, typ, point, context, make(map[valueVisit]bool), make(map[memoryVisit]bool))
	}
}

func returnedStorage(t testing.TB, function *ssa.Function, name string) (*ssa.Alloc, *ssa.Return) {
	t.Helper()
	var storage *ssa.Alloc
	var result *ssa.Return
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			if allocation, ok := instruction.(*ssa.Alloc); ok && allocation.Comment == name {
				storage = allocation
			}
			if statement, ok := instruction.(*ssa.Return); ok {
				result = statement
			}
		}
	}
	if storage == nil || result == nil {
		t.Fatal("fixture lost its storage or return")
	}
	return storage, result
}
