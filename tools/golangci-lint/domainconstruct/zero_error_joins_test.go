package domainconstruct

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func TestErrorJoinContextsStayIndependent(t *testing.T) {
	c, guard, returned := errorJoinFlow(t, `
if err != nil { err = &failure{} }
`)
	base := c.contextAt(returned.Block())
	contexts := make(map[*ssa.BasicBlock]flowContext)
	for block, context := range c.contexts {
		contexts[block] = context.clone()
	}
	for _, state := range []truth{isZero, notZero, isZero} {
		context := base.clone()
		if !c.assume(&context, returned.Results[1], state) {
			t.Fatal("independent assumption is unexpectedly impossible")
		}
		facts := maps.Clone(context.facts)
		if got := c.errorGuardChecked(guard, context); got != (state == isZero) {
			t.Errorf("forwarded error state %v: original error checked = %v", state, got)
		}
		if !context.possible || !maps.Equal(context.facts, facts) {
			t.Fatal("query changed its caller's assumptions")
		}
	}
	for block, before := range contexts {
		after := c.contextAt(block)
		if after.possible != before.possible || !maps.Equal(after.facts, before.facts) {
			t.Errorf("query changed cached facts for block %d", block.Index)
		}
	}
	if c.errorGuardChecked(guard, base) {
		t.Error("a previous query supplied an unchecked error's nilness")
	}
}

func TestErrorJoinProofBounds(t *testing.T) {
	for _, joins := range []int{8, 256} {
		t.Run(fmt.Sprintf("joins=%d", joins), func(t *testing.T) {
			var body strings.Builder
			for index := range joins {
				fmt.Fprintf(&body, "if flags[%d] { err = &failure{} }\n", index)
			}
			c, guard, returned := errorJoinFlow(t, body.String())
			context := c.contextAt(returned.Block()).clone()
			c.assume(&context, returned.Results[1], isZero)
			allocations := testing.AllocsPerRun(3, func() {
				checked := c.errorGuardChecked(guard, context)
				if joins == 8 && !checked {
					t.Fatal("ordinary sequential error replacements lost the nil guard")
				}
			})
			// Large functions may retain a warning. They must not enumerate the
			// exponentially many paths through independent replacements.
			if allocations > 4000 {
				t.Errorf("error tracing across %d joins allocates %.0f times", joins, allocations)
			}
		})
	}
}

func errorJoinFlow(t testing.TB, body string) (*zeroChecker, *ssa.Extract, *ssa.Return) {
	t.Helper()
	function := buildFlowFunction(t, `package flow
type failure struct{}
func (*failure) Error() string { return "failed" }
func run(lookup func() (string, error), flags []bool) (string, error) {
  value, err := lookup()
`+body+`
  return value, err
}`)
	var guard *ssa.Extract
	var returned *ssa.Return
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			switch value := instruction.(type) {
			case *ssa.Extract:
				if value.Index == 1 {
					guard = value
				}
			case *ssa.Return:
				returned = value
			}
		}
	}
	if guard == nil || returned == nil {
		t.Fatal("fixture lost its call's error or return")
	}
	return newFlowChecker(function), guard, returned
}
