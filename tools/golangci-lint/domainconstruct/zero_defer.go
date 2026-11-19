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
	first int // Registrations at or above first have already executed.
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
		return
	}
	after := flowPoint{point.block, point.index + 1}
	for index, call := range slices.Backward(calls) {
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
				execution := executedCall{call: call, before: point, context: context, conditional: !ordered}
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
// Other registrations retain possible effects without establishing a write.
func (c *zeroChecker) defersAt(point flowPoint) ([]*ssa.Defer, bool) {
	calls := c.functionDefers(point.block.Parent())
	if len(calls) > maxOrderedDefers {
		return calls, false
	}
	for _, call := range calls {
		if call.Block() != point.block || instructionPoint(call).index >= point.index {
			return calls, false
		}
	}
	return calls, true
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
