package domainconstruct

import (
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// A single registration in the cleanup block has a known execution order.
// Conditional registration and interacting deferred calls have opaque effects.
func (c *zeroChecker) deferredAt(point flowPoint) *ssa.Defer {
	function := point.block.Parent()
	deferred, indexed := c.deferred[function]
	if !indexed {
		deferred = singleDefer(function)
		if c.deferred == nil {
			c.deferred = make(map[*ssa.Function]*ssa.Defer)
		}
		c.deferred[function] = deferred
	}
	if deferred != nil && deferred.Block() == point.block && instructionPoint(deferred).index < point.index {
		return deferred
	}
	return nil
}

func singleDefer(function *ssa.Function) *ssa.Defer {
	var deferred *ssa.Defer
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			if call, ok := instruction.(*ssa.Defer); ok {
				if deferred != nil {
					return nil
				}
				deferred = call
			}
		}
	}
	return deferred
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
