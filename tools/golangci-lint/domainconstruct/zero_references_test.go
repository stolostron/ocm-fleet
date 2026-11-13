package domainconstruct

import (
	"go/constant"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestSourceReferences(t *testing.T) {
	function := buildFlowFunction(t, `package flow
type Record struct { ID string }
func run(input *struct { Items [2]Record; Rows []Record }, scalar, raw string, keep func(any)) func() string {
  var record Record
  keep(&record)
  return func() string { return raw }
}`)
	// No syntax is supplied to this checker. Source names retained by SSA are
	// still useful, but an allocation's comment is insufficient evidence.
	c := newFlowChecker(function)
	input, scalar := function.Params[0], function.Params[1]
	record := function.Pkg.Type("Record").Type()
	var storage *ssa.Alloc
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			if allocation, ok := instruction.(*ssa.Alloc); ok &&
				types.Identical(allocation.Type().Underlying().(*types.Pointer).Elem(), record) {
				storage = allocation
			}
		}
	}
	if storage == nil || len(function.AnonFuncs) != 1 || len(function.AnonFuncs[0].FreeVars) != 1 {
		t.Fatal("fixture lost its storage or captured input")
	}
	tests := []struct {
		name        string
		value       ssa.Value
		path        []int
		expression  string
		description string
	}{
		{"parameter", input, nil, "input", "input"},
		{"fixed array member", input, []int{0, 1, 0}, "input.Items[1].ID", "input.Items[1].ID"},
		{"symbolic slice member", input, []int{1, -1, 0}, "input", "input at path Rows[index].ID"},
		{"out of range field", input, []int{2}, "input", "input"},
		{"out of range element", input, []int{0, 2, 0}, "input", "input"},
		{"scalar has no members", scalar, []int{0}, "scalar", "scalar"},
		{"captured input", function.AnonFuncs[0].FreeVars[0], nil, "raw", "raw"},
		{"allocation without syntax", storage, []int{0}, "", "unnamed storage at path ID"},
		{"zero struct", ssa.NewConst(nil, record), nil, "", "unnamed storage"},
		{"zero struct member", ssa.NewConst(nil, record), []int{0}, "", "unnamed storage"},
		{"zero array", ssa.NewConst(nil, types.NewArray(record, 1)), nil, "", "unnamed storage"},
		{"nil pointer", ssa.NewConst(nil, types.NewPointer(record)), nil, "nil", "nil"},
		{"nil cannot name a member", ssa.NewConst(nil, types.NewPointer(record)), []int{0}, "", "unnamed storage"},
		{"empty string", ssa.NewConst(constant.MakeString(""), types.Typ[types.String]), nil, `""`, `""`},
		{"missing value", nil, nil, "", "unnamed storage"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := c.valueName(test.value, test.path); got != test.expression {
				t.Errorf("source expression = %q, want %q", got, test.expression)
			}
			if got := c.storageName(test.value, test.path); got != test.description {
				t.Errorf("storage description = %q, want %q", got, test.description)
			}
		})
	}
}

func TestCallNameWithoutSource(t *testing.T) {
	function := buildFlowFunction(t, `package flow
func run(factory func() func(*string)) {
  var raw string
  factory()(&raw)
}`)
	c := newFlowChecker(function)
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			call, ok := instruction.(*ssa.Call)
			if !ok {
				continue
			}
			if _, indirect := call.Call.Value.(*ssa.Call); indirect {
				if got := c.callName(&call.Call); got != "call" {
					t.Errorf("indirect call without syntax = %q, want call", got)
				}
				return
			}
		}
	}
	t.Fatal("fixture lost its indirect call")
}

func TestBoundMethodCapturesHaveNoSourceNames(t *testing.T) {
	function := buildFlowFunction(t, `package flow
type Record struct{}
func (Record) Apply(string) {}
func run(input Record) func(string) { return input.Apply }
`)
	c := newFlowChecker(function)
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			closure, ok := instruction.(*ssa.MakeClosure)
			if !ok {
				continue
			}
			wrapper := closure.Fn.(*ssa.Function)
			if len(wrapper.FreeVars) != 1 || len(wrapper.Params) != 1 {
				t.Fatal("fixture lost its captured receiver or unnamed parameter")
			}
			for _, value := range []ssa.Value{wrapper.FreeVars[0], wrapper.Params[0]} {
				if got := c.valueName(value, nil); got != "" {
					t.Errorf("generated %T has source name %q, want unknown", value, got)
				}
			}
			return
		}
	}
	t.Fatal("fixture lost its bound method")
}
