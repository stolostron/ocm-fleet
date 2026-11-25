package domainconstruct

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestGuardedReadEffects(t *testing.T) {
	tests := []struct {
		name      string
		field     string
		effect    string
		unchanged bool
	}{
		{"unchanged field", "input.ID", "", true},
		{"sibling write", "input.ID", `input.Note = "changed"`, true},
		{"field write", "input.ID", `input.ID = ""`, false},
		{"whole value write", "input.ID", `*input = Record{}`, false},
		{"possible parameter alias", "input.ID", `other.ID = ""`, false},
		{"borrowed pointer write", "input.ID", `*alias = ""`, false},
		{"unknown call", "input.ID", `_ = borrow(&input.ID)`, false},
		{"dynamic element write", "input.Items[0]", `input.Items[index] = ""`, false},
		{"clear elements", "input.Items[0]", `clear(input.Items[:])`, false},
		{"copy elements", "input.Items[0]", `copy(input.Items[:], []string{""})`, false},
		{"append within capacity", "input.Items[0]", `_ = append(input.Items[:0], "")`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			function := buildFlowFunction(t, fmt.Sprintf(`package flow
type Record struct { ID string; Note string; Items [1]string }
func run(input, other *Record, borrow func(*string) *string, index int) string {
  alias := borrow(&%s)
  _ = alias
  if %s == "" { return "" }
  %s
  return %s
}`, test.field, test.field, test.effect, test.field))
			checkGuardedRead(t, function, test.unchanged)
		})
	}
}

func TestGuardedReadAcrossIndependentAllocations(t *testing.T) {
	function := buildFlowFunction(t, `package flow
type Record struct { ID string; Note string }
func run(raw string, consume func(*Record)) string {
  var input Record
  input.ID = raw
  if input.ID == "" { return "" }
  var other Record
  other.Note = "changed"
  consume(&other)
  return input.ID
}`)
	checkGuardedRead(t, function, true)
}

func TestUnchangedReadSkipsUnrelatedFacts(t *testing.T) {
	var source strings.Builder
	source.WriteString(`package flow
type Record struct { ID string }
func run(input *Record, index int, mark func()) string {
`)
	for index := range 32 {
		fmt.Fprintf(&source, "if index == %d { mark() }\n", index)
	}
	source.WriteString("return input.ID\n}")
	function := buildFlowFunction(t, source.String())
	c := newFlowChecker(function)
	context := flowContext{facts: make(map[ssa.Value]truth), possible: true}
	var read ssa.Value
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			switch instruction := instruction.(type) {
			case *ssa.BinOp:
				context.facts[instruction] = notZero
			case *ssa.Return:
				read = instruction.Results[0]
			}
		}
	}
	if read == nil || len(context.facts) != 32 {
		t.Fatal("fixture lost its field read or comparison facts")
	}
	allocations := testing.AllocsPerRun(3, func() {
		if state := c.unchangedReadFact(read, context); state != unknown {
			t.Fatalf("unrelated facts establish %v, want unknown", state)
		}
	})
	if allocations != 0 {
		t.Errorf("unrelated facts cause %.0f storage-path allocations, want none", allocations)
	}
}

func TestUnchangedReadReusesLocationsAcrossBranchFacts(t *testing.T) {
	function := buildFlowFunction(t, `package flow
type Record struct { ID string }
func run(input *Record) string {
  if input.ID == "" { return "" }
  return input.ID
}`)
	var reads []*ssa.UnOp
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			if read, ok := instruction.(*ssa.UnOp); ok {
				reads = append(reads, read)
			}
		}
	}
	if len(reads) != 2 {
		t.Fatal("fixture lost its two field reads")
	}
	c := newFlowChecker(function)
	context := flowContext{facts: make(map[ssa.Value]truth), possible: true}
	for _, state := range []truth{notZero, isZero} {
		context.facts[reads[0]] = state
		allocations := testing.AllocsPerRun(3, func() {
			if got := c.unchangedReadFact(reads[1], context); got != state {
				t.Fatalf("read fact = %v, want current branch fact %v", got, state)
			}
		})
		if allocations != 0 {
			t.Errorf("repeated read-path resolution allocates %.0f times, want none", allocations)
		}
	}
}

func checkGuardedRead(t *testing.T, function *ssa.Function, unchanged bool) {
	t.Helper()
	c := newFlowChecker(function)
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			result, ok := instruction.(*ssa.Return)
			if !ok {
				continue
			}
			if value, load := result.Results[0].(*ssa.UnOp); load {
				state := c.valueTruth(value, c.contextAt(block), make(map[ssa.Value]bool))
				if (state == notZero) != unchanged {
					t.Errorf("nonzero fact preserved = %v, want %v", state == notZero, unchanged)
				}
				return
			}
		}
	}
	t.Fatal("fixture lost the returned field read")
}
