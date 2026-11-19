package domainconstruct

import (
	"go/constant"
	"go/token"
	"go/types"
	"maps"

	"golang.org/x/tools/go/ssa"
)

// truth describes zero-ness, including nilness and boolean conditions. A
// nonzero raw value still needs construction to establish other invariants.
type truth uint8

const (
	unknown truth = iota
	isZero
	notZero
)

type flowContext struct {
	facts    map[ssa.Value]truth
	possible bool
	use      valueUse
}

func (context flowContext) clone() flowContext {
	context.facts = maps.Clone(context.facts)
	return context
}

func (context flowContext) cloneFor(additionalFacts int) flowContext {
	facts := make(map[ssa.Value]truth, len(context.facts)+additionalFacts)
	maps.Copy(facts, context.facts)
	context.facts = facts
	return context
}

func (c *zeroChecker) contextAt(block *ssa.BasicBlock) flowContext {
	if context, found := c.contexts[block]; found {
		return context
	}
	context := flowContext{facts: make(map[ssa.Value]truth), possible: true}
	if parent := block.Idom(); parent != nil {
		context = c.contextAt(parent).clone()
	}
	// Publish the dominator facts before following a surviving predecessor:
	// an interface's non-returning call can remove an edge from a loop.
	c.contexts[block] = context
	var predecessor *ssa.BasicBlock
	count := 0
	for _, candidate := range block.Preds {
		if !testingExitBlock(candidate) {
			predecessor = candidate
			count++
		}
	}
	if count == 1 {
		context = c.contextAt(predecessor).clone()
		c.assumeEdge(&context, predecessor, block)
	} else if count == 0 && len(block.Preds) != 0 {
		context.possible = false
	}
	c.contexts[block] = context
	return context
}

func (c *zeroChecker) edgeContext(context flowContext, block *ssa.BasicBlock, index int) flowContext {
	predecessor := block.Preds[index]
	// Reserve room for the incoming assumptions before replaying them. Queries
	// far beyond this predecessor can carry many more facts than it dominates.
	edge := c.contextAt(predecessor).cloneFor(len(context.facts))
	edge.use = context.use
	if testingExitBlock(predecessor) {
		edge.possible = false
		return edge
	}
	c.assumeEdge(&edge, predecessor, block)
	backEdge := block.Dominates(predecessor)
	for value, state := range context.facts {
		if instruction, ok := value.(ssa.Instruction); backEdge && ok && instruction.Block() != nil && block.Dominates(instruction.Block()) {
			continue // The loop-local fact describes a different iteration.
		}
		if phi, ok := value.(*ssa.Phi); ok && phi.Block() == block {
			value = phi.Edges[index]
		}
		c.assume(&edge, value, state)
	}
	return edge
}

func (c *zeroChecker) assumeEdge(context *flowContext, predecessor, successor *ssa.BasicBlock) {
	if len(predecessor.Instrs) == 0 {
		return
	}
	if branch, ok := predecessor.Instrs[len(predecessor.Instrs)-1].(*ssa.If); ok {
		state := isZero
		if predecessor.Succs[0] == successor {
			state = notZero
		}
		c.assume(context, branch.Cond, state)
	}
}

func (c *zeroChecker) assume(context *flowContext, value ssa.Value, state truth) bool {
	if !context.possible || context.facts[value] == state {
		return context.possible
	}
	if actual := c.valueTruth(value, *context, make(map[ssa.Value]bool)); actual != unknown && actual != state {
		context.possible = false
		return false
	}
	context.facts[value] = state
	if origin := localLoadOrigin(value); origin != nil {
		return c.assume(context, origin, state)
	}
	switch value := value.(type) {
	case *ssa.ChangeInterface:
		return c.assume(context, value.X, state)
	case *ssa.ChangeType:
		return c.assume(context, value.X, state)
	case *ssa.Call:
		if fact := c.callContract(&value.Call); fact != nil {
			for _, rule := range fact.Errors {
				if rule.Result == 0 && rule.Parameter >= 0 && (state == isZero || rule.Exact) {
					if argument := contractArgument(&value.Call, rule.Parameter); argument != nil {
						c.assume(context, argument, state)
					}
				}
			}
		}
	case *ssa.UnOp:
		if value.Op == token.NOT {
			return c.assume(context, value.X, opposite(state))
		}
	case *ssa.BinOp:
		if value.Op == token.EQL || value.Op == token.NEQ {
			if value.Op == token.NEQ {
				state = opposite(state)
			}
			for _, pair := range [][2]ssa.Value{{value.X, value.Y}, {value.Y, value.X}} {
				if literal, ok := pair[1].(*ssa.Const); ok {
					if zeroConstant(literal) {
						return c.assume(context, pair[0], opposite(state))
					}
					if literal.Value.Kind() == constant.Bool {
						if !constant.BoolVal(literal.Value) {
							state = opposite(state)
						}
						return c.assume(context, pair[0], state)
					}
				}
			}
		}
	}
	c.refineBooleanEqualities(context, value.Parent())
	return context.possible
}

// Equality between two booleans carries information in both directions. A
// table's wantErr check can establish err == nil after a fatal assertion even
// though neither operand of the earlier comparison was a constant.
func (c *zeroChecker) refineBooleanEqualities(context *flowContext, function *ssa.Function) {
	for _, comparison := range c.booleanEqualities(function) {
		state := context.facts[comparison]
		if state == unknown {
			continue
		}
		equal := state == notZero
		if comparison.Op == token.NEQ {
			equal = !equal
		}
		for _, pair := range [][2]ssa.Value{{comparison.X, comparison.Y}, {comparison.Y, comparison.X}} {
			known := c.valueTruth(pair[0], *context, make(map[ssa.Value]bool))
			if known != unknown {
				if !equal {
					known = opposite(known)
				}
				if !c.assume(context, pair[1], known) {
					return
				}
			}
		}
	}
}

// Only boolean comparisons can refine neighboring facts. Index them once so
// replaying an assumption does not scan every unrelated fact in the context.
func (c *zeroChecker) booleanEqualities(function *ssa.Function) []*ssa.BinOp {
	if function == nil {
		return nil
	}
	if comparisons, found := c.equalities[function]; found {
		return comparisons
	}
	var comparisons []*ssa.BinOp
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			comparison, ok := instruction.(*ssa.BinOp)
			if !ok || comparison.Op != token.EQL && comparison.Op != token.NEQ {
				continue
			}
			if basic, ok := comparison.X.Type().Underlying().(*types.Basic); ok && basic.Info()&types.IsBoolean != 0 {
				comparisons = append(comparisons, comparison)
			}
		}
	}
	if c.equalities == nil {
		c.equalities = make(map[*ssa.Function][]*ssa.BinOp)
	}
	c.equalities[function] = comparisons
	return comparisons
}

func opposite(state truth) truth {
	if state == isZero {
		return notZero
	}
	return isZero
}

func (c *zeroChecker) valueTruth(value ssa.Value, context flowContext, seen map[ssa.Value]bool) truth {
	return c.truthInContext(value, context, seen, nil)
}

func (c *zeroChecker) truthInContext(value ssa.Value, context flowContext, seen map[ssa.Value]bool, completed map[ssa.Value]truth) (result truth) {
	if state := context.facts[value]; state != unknown {
		return state
	}
	if state := completed[value]; state != unknown {
		return state
	}
	if state := c.unchangedReadFact(value, context); state != unknown {
		return state
	}
	if seen[value] || !c.work.take() {
		return unknown
	}
	seen[value] = true
	defer delete(seen, value)
	if completed != nil {
		defer func() {
			if result != unknown {
				completed[value] = result
			}
		}()
	}
	if origin := localLoadOrigin(value); origin != nil {
		return c.truthInContext(origin, context, seen, completed)
	}
	switch value := value.(type) {
	case *ssa.Const:
		if zeroConstant(value) {
			return isZero
		}
		return notZero
	case *ssa.Alloc, *ssa.MakeChan, *ssa.MakeMap, *ssa.MakeSlice:
		return notZero
	case *ssa.MakeInterface:
		if _, generic := types.Unalias(value.X.Type()).(*types.TypeParam); !generic {
			return notZero
		}
	case *ssa.ChangeInterface:
		return c.truthInContext(value.X, context, seen, completed)
	case *ssa.ChangeType:
		return c.truthInContext(value.X, context, seen, completed)
	case *ssa.UnOp:
		if value.Op == token.NOT {
			if state := c.truthInContext(value.X, context, seen, completed); state != unknown {
				return opposite(state)
			}
		}
		if value.Op == token.MUL {
			if global, ok := value.X.(*ssa.Global); ok {
				if variable, ok := global.Object().(*types.Var); ok && c.contracts.sentinels[variable] && len(c.contracts.writes[variable]) == 0 {
					return notZero
				}
			}
		}
	case *ssa.Phi:
		if completed == nil {
			completed = make(map[ssa.Value]truth)
		}
		state := unknown
		for index, edge := range value.Edges {
			actual := c.truthInContext(edge, context, seen, completed)
			if actual == unknown || index > 0 && actual != state {
				return unknown
			}
			state = actual
		}
		return state
	case *ssa.Call:
		return c.callTruth(value, 0, context, seen)
	case *ssa.Extract:
		if call, ok := value.Tuple.(*ssa.Call); ok {
			return c.callTruth(call, value.Index, context, seen)
		}
	}
	return unknown
}

// Static testing calls already use ctrlflow's non-return facts in buildssa.
// A wrapper embedding testing.TB can override the method and return.
func testingExit(call *ssa.CallCommon) bool {
	if !call.IsInvoke() || call.Method.Pkg() == nil || call.Method.Pkg().Path() != "testing" {
		return false
	}
	switch call.Method.Name() {
	case "Fatal", "Fatalf", "FailNow", "Skip", "Skipf", "SkipNow":
		for receiver := call.Value; ; {
			switch value := receiver.(type) {
			case *ssa.ChangeInterface:
				receiver = value.X
			case *ssa.MakeInterface:
				named := domainType(value.X.Type())
				return named != nil && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "testing" &&
					(named.Obj().Name() == "T" || named.Obj().Name() == "B" || named.Obj().Name() == "F")
			default:
				return false
			}
		}
	}
	return false
}

func testingExitBlock(block *ssa.BasicBlock) bool {
	for _, instruction := range block.Instrs {
		if call, ok := instruction.(*ssa.Call); ok && testingExit(&call.Call) {
			return true
		}
	}
	return false
}

// localLoadOrigin recovers the last assignment in this block. Escaping a local
// does not invalidate a subsequent direct assignment, but intervening effects
// must not be crossed. Unescaped return slots can also cross deferred cleanup.
func localLoadOrigin(value ssa.Value) ssa.Value {
	load, ok := value.(*ssa.UnOp)
	if !ok || load.Op != token.MUL {
		return nil
	}
	allocation, ok := load.X.(*ssa.Alloc)
	if !ok {
		return nil
	}
	escaped := false
	for _, reference := range *allocation.Referrers() {
		switch reference := reference.(type) {
		case *ssa.Store:
			if reference.Addr != allocation || reference.Val == allocation {
				escaped = true
			}
		case *ssa.UnOp:
			if reference.Op != token.MUL || reference.X != allocation {
				escaped = true
			}
		case *ssa.DebugRef:
		default:
			escaped = true
		}
	}
	point := instructionPoint(load)
	for index := point.index - 1; index >= 0; index-- {
		instruction := point.block.Instrs[index]
		if store, ok := instruction.(*ssa.Store); ok && store.Addr == allocation {
			return store.Val
		}
		if escaped {
			if channelHandoff(instruction) {
				return nil
			}
			switch instruction.(type) {
			case ssa.CallInstruction, *ssa.RunDefers, *ssa.Store:
				return nil
			}
		}
	}
	return nil
}
