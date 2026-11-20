package domainconstruct

import (
	"go/token"
	"go/types"
	"iter"
	"slices"

	"golang.org/x/tools/go/ssa"
)

const maxOrderedDefers = 8

type cleanupPosition struct {
	point flowPoint
	first int // Only registrations at or above first can precede this read.
}

type executedCall struct {
	call        ssa.CallInstruction
	before      flowPoint
	context     flowContext
	conditional bool
}

func (c *zeroChecker) checkCleanup(point flowPoint, context flowContext) {
	calls, ordered := c.defersAt(point)
	if !ordered {
		if len(calls) != 1 {
			return
		}
		registration := calls[0].Block()
		if c.blockReachedFromPredecessors(registration, registration.Preds) != isZero {
			return
		}
		// With one reachable, nonrepeating registration, cleanup runs the
		// call only on paths where the registration's guards held.
		registered := c.contextAt(registration)
		if !registered.possible {
			return
		}
		context = context.cloneFor(len(registered.facts))
		for value, state := range registered.facts {
			if !c.work.take() || !c.assume(&context, value, state) {
				return
			}
		}
	}
	after := flowPoint{point.block, point.index + 1}
	for index, call := range slices.Backward(calls) {
		if ordered && !deferPrecedesPoint(call, point) {
			continue
		}
		if !c.work.take() {
			return
		}
		context.cleanup = cleanupPosition{point, index + 1}
		c.checkCall(call, after, context, true)
	}
}

// Follow actual execution backwards. A deferred call's arguments are captured
// at registration, and its prior storage includes cleanup calls that ran first.
func (c *zeroChecker) executedCallsAt(instruction ssa.Instruction, point flowPoint, context flowContext) iter.Seq[executedCall] {
	return func(yield func(executedCall) bool) {
		switch instruction := instruction.(type) {
		case *ssa.Defer:
			return
		case *ssa.RunDefers:
			calls, ordered := c.defersAt(point)
			first := 0
			if context.cleanup.point == point {
				first = context.cleanup.first
			}
			for index, call := range calls[first:] {
				if !c.work.take() {
					return
				}
				execution := executedCall{call: call, before: point, context: context, conditional: !ordered || !deferPrecedesPoint(call, point)}
				if ordered {
					execution.before.index++
					execution.context.cleanup = cleanupPosition{point, first + index + 1}
				}
				if !yield(execution) {
					return
				}
			}
		case ssa.CallInstruction:
			yield(executedCall{call: instruction, before: point, context: context})
		}
	}
}

// A small set of registrations in the cleanup block has a known LIFO order.
// A single dominating registration outside a cycle also executes exactly once.
func (c *zeroChecker) defersAt(point flowPoint) ([]*ssa.Defer, bool) {
	calls := c.functionDefers(point.block.Parent())
	if len(calls) == 1 && calls[0].Block() != point.block {
		registration := calls[0].Block()
		if registration.Dominates(point.block) {
			return calls, c.blockReachedFromPredecessors(registration, registration.Preds) == isZero
		}
		if c.blockReachedFromPredecessors(registration, point.block.Preds) == isZero {
			return nil, true
		}
		return calls, false
	}
	if len(calls) > maxOrderedDefers {
		return calls, false
	}
	for _, call := range calls {
		if call.Block() != point.block || instructionPoint(call).index >= point.index {
			if len(calls) == 2 {
				return c.orderedDeferPair(calls, point)
			}
			return calls, false
		}
	}
	return calls, true
}

// A definite registration followed by one possible registration has a fixed
// cleanup order when neither registration can repeat.
func (c *zeroChecker) orderedDeferPair(calls []*ssa.Defer, point flowPoint) ([]*ssa.Defer, bool) {
	first, second := calls[0], calls[1]
	if deferPrecedesPoint(second, instructionPoint(first)) {
		first, second = second, first
	}
	if !deferPrecedesPoint(first, point) || !deferPrecedesPoint(first, instructionPoint(second)) {
		return calls, false
	}
	if c.blockReachedFromPredecessors(first.Block(), first.Block().Preds) != isZero ||
		c.blockReachedFromPredecessors(second.Block(), second.Block().Preds) != isZero {
		return calls, false
	}
	if second.Block() != point.block && c.blockReachedFromPredecessors(second.Block(), point.block.Preds) == isZero {
		return []*ssa.Defer{first}, true
	}
	return []*ssa.Defer{first, second}, true
}

func deferPrecedesPoint(call *ssa.Defer, point flowPoint) bool {
	if call.Block() == point.block {
		return instructionPoint(call).index < point.index
	}
	return call.Block().Dominates(point.block)
}

func (c *zeroChecker) blockReachedFromPredecessors(target *ssa.BasicBlock, predecessors []*ssa.BasicBlock) truth {
	if len(predecessors) == 0 {
		return isZero
	}
	pending := slices.Clone(predecessors)
	seen := make(map[*ssa.BasicBlock]bool)
	for len(pending) != 0 {
		if !c.work.take() {
			return unknown
		}
		block := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if block == target {
			return notZero
		}
		if !seen[block] {
			seen[block] = true
			pending = append(pending, block.Preds...)
		}
	}
	return isZero
}

func (c *zeroChecker) functionDefers(function *ssa.Function) []*ssa.Defer {
	if calls, indexed := c.deferred[function]; indexed {
		return calls
	}
	var calls []*ssa.Defer
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			if call, ok := instruction.(*ssa.Defer); ok {
				calls = append(calls, call)
			}
		}
	}
	if c.deferred == nil {
		c.deferred = make(map[*ssa.Function][]*ssa.Defer)
	}
	c.deferred[function] = calls
	return calls
}

func (c *zeroChecker) deferChangesStatus(status ssa.Value) bool {
	read, loaded := status.(*ssa.UnOp)
	if !loaded || read.Op != token.MUL {
		return false
	}
	calls := c.functionDefers(read.Parent())
	if len(calls) == 0 {
		return false
	}
	location, known := c.collectionAddress(read.X)
	if !known {
		return true
	}
	for _, call := range calls {
		if !c.work.take() || c.callMayMutate(call.Common(), location, instructionPoint(call)) {
			return true
		}
	}
	return false
}

func deferredReference(value ssa.Value) bool {
	for {
		switch boxed := value.(type) {
		case *ssa.MakeInterface:
			value = boxed.X
		case *ssa.ChangeInterface:
			value = boxed.X
		default:
			return hasReferencedStorage(value.Type(), make(map[types.Type]bool))
		}
	}
}

func hasReferencedStorage(t types.Type, seen map[types.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	switch shape := t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Signature, *types.Interface:
		return true
	case *types.Struct:
		for field := range shape.Fields() {
			if hasReferencedStorage(field.Type(), seen) {
				return true
			}
		}
	case *types.Array:
		return shape.Len() != 0 && hasReferencedStorage(shape.Elem(), seen)
	}
	return false
}
