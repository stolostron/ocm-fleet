package domainconstruct

import (
	"go/token"

	"golang.org/x/tools/go/ssa"
)

// filledByLoop recognizes one complete sequential fill. The loop must have no
// break into its exit and must write every element on every back edge. Error
// checks after an assignment are evaluated at the back edge, so early returns
// on parse failure do not invalidate a successful fill.
func (c *zeroChecker) filledByLoop(view collectionView, header, exit *ssa.BasicBlock, seen map[valueVisit]bool) bool {
	if len(exit.Preds) != 1 || len(header.Instrs) == 0 {
		return false
	}
	branch, ok := header.Instrs[len(header.Instrs)-1].(*ssa.If)
	if !ok || header.Succs[1] != exit {
		return false
	}
	condition, ok := branch.Cond.(*ssa.BinOp)
	if !ok || condition.Op != token.LSS || !sequentialIndex(condition.X, header) || !c.lengthCoversView(condition.Y, view) {
		return false
	}
	var backEdges []*ssa.BasicBlock
	for _, predecessor := range header.Preds {
		if header.Dominates(predecessor) {
			backEdges = append(backEdges, predecessor)
		}
	}
	if len(backEdges) == 0 {
		return false
	}
	assignments := make(map[*ssa.Store]bool)
	for _, block := range header.Parent().Blocks {
		if !header.Dominates(block) || exit.Dominates(block) {
			continue
		}
		for index, instruction := range block.Instrs {
			if call, ok := instruction.(ssa.CallInstruction); ok {
				point := flowPoint{block, index}
				if builtin, builtinCall := call.Common().Value.(*ssa.Builtin); builtinCall {
					if builtin.Name() == "copy" || builtin.Name() == "append" || builtin.Name() == "clear" {
						if c.mayReferenceStorage(call.Common().Args[0], mutationTarget{view.storage, &elementRange{view.start, view.end}}, point, make(map[ssa.Value]bool)) {
							return false
						}
					}
				} else if c.collectionMutation(call.Common(), view, point) != mutationReadOnly {
					return false
				}
			}
			if store, ok := instruction.(*ssa.Store); ok {
				if location, ok := c.collectionAddress(store.Addr); ok && location.root == view.storage.root && pathPrefix(location.path, view.storage.path) {
					indexed, ok := store.Addr.(*ssa.IndexAddr)
					if !ok || indexed.Index != condition.X {
						return false
					}
					target, ok := c.collectionOf(indexed.X)
					if !ok || target.start != 0 || !sameStorage(target.storage, view.storage) {
						return false
					}
					assignments[store] = true
				}
			}
		}
	}
	if len(assignments) == 0 {
		return false
	}
	for _, backEdge := range backEdges {
		context := c.contextAt(backEdge).clone()
		c.assumeEdge(&context, backEdge, header)
		if !c.filledOnPaths(view, header, backEdge, assignments, context, seen, make(map[*ssa.BasicBlock]bool)) {
			return false
		}
	}
	return true
}

func (c *zeroChecker) filledOnPaths(view collectionView, header, block *ssa.BasicBlock, assignments map[*ssa.Store]bool, context flowContext, values map[valueVisit]bool, seen map[*ssa.BasicBlock]bool) bool {
	if block == header || seen[block] {
		return false
	}
	seen[block] = true
	defer delete(seen, block)
	for index := len(block.Instrs) - 1; index >= 0; index-- {
		if store, ok := block.Instrs[index].(*ssa.Store); ok && assignments[store] {
			return c.problem(store.Val, nil, view.element, flowPoint{block, index}, context, values) == nil
		}
	}
	found := false
	for index, predecessor := range block.Preds {
		edge := c.edgeContext(context, block, index)
		if !edge.possible {
			continue
		}
		found = true
		if !c.filledOnPaths(view, header, predecessor, assignments, edge, values, seen) {
			return false
		}
	}
	return found
}

func sequentialIndex(value ssa.Value, header *ssa.BasicBlock) bool {
	phi, direct := value.(*ssa.Phi)
	initial := int64(0)
	if !direct {
		increment, ok := value.(*ssa.BinOp)
		if !ok || increment.Op != token.ADD {
			return false
		}
		one, known := integerValue(increment.Y)
		phi, ok = increment.X.(*ssa.Phi)
		if !ok || !known || one != 1 {
			return false
		}
		initial = -1
	}
	if phi.Block() != header {
		return false
	}
	entry, back := false, false
	for _, incoming := range phi.Edges {
		if number, known := integerValue(incoming); known && number == initial {
			entry = true
			continue
		}
		if direct {
			increment, ok := incoming.(*ssa.BinOp)
			if !ok || increment.Op != token.ADD || increment.X != phi {
				return false
			}
			one, known := integerValue(increment.Y)
			if !known || one != 1 {
				return false
			}
		} else if incoming != value {
			return false
		}
		back = true
	}
	return entry && back
}

func (c *zeroChecker) lengthCoversView(length ssa.Value, view collectionView) bool {
	return c.lengthCoversViewSeen(length, view, make(map[ssa.Value]bool))
}

func (c *zeroChecker) lengthCoversViewSeen(length ssa.Value, view collectionView, seen map[ssa.Value]bool) bool {
	if length == nil || seen[length] {
		return false
	}
	seen[length] = true
	if number, known := integerValue(length); known {
		return view.end >= 0 && number >= view.end
	}
	if sameLength(length, view.length) {
		return true
	}
	if call, ok := length.(*ssa.Call); ok {
		if builtin, ok := call.Call.Value.(*ssa.Builtin); ok && builtin.Name() == "len" {
			if call.Call.Args[0] == view.value && view.start == 0 {
				return true
			}
			if bound, ok := c.collectionOf(call.Call.Args[0]); ok && sameStorage(bound.storage, view.storage) && bound.start == 0 {
				return bound.end >= 0 && view.end >= 0 && bound.end >= view.end || sameLength(bound.length, view.length)
			}
			if producer, result, ok := sliceProducer(call.Call.Args[0]); ok {
				if fact := c.callContract(&producer.Call); fact != nil {
					for _, bound := range fact.Lengths {
						if bound.Result != result {
							continue
						}
						if bound.Length.Parameter < 0 {
							return view.end >= 0 && bound.Length.Constant >= view.end
						}
						argument := sourceArgument(&producer.Call, bound.Length.Parameter)
						if !bound.Length.FromLen {
							return c.lengthCoversViewSeen(argument, view, seen)
						}
						if size, ok := view.length.(*ssa.Call); ok {
							if builtin, ok := size.Call.Value.(*ssa.Builtin); ok && builtin.Name() == "len" && sameSliceValue(size.Call.Args[0], argument) {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

func sliceProducer(value ssa.Value) (*ssa.Call, int, bool) {
	switch value := value.(type) {
	case *ssa.Call:
		return value, 0, true
	case *ssa.Extract:
		if call, ok := value.Tuple.(*ssa.Call); ok {
			return call, value.Index, true
		}
	}
	return nil, 0, false
}

func sameLength(first, second ssa.Value) bool {
	if first == nil || second == nil {
		return false
	}
	if first == second {
		return true
	}
	left, leftCall := first.(*ssa.Call)
	right, rightCall := second.(*ssa.Call)
	if leftCall && rightCall {
		leftBuiltin, leftLen := left.Call.Value.(*ssa.Builtin)
		rightBuiltin, rightLen := right.Call.Value.(*ssa.Builtin)
		return leftLen && rightLen && leftBuiltin.Name() == "len" && rightBuiltin.Name() == "len" && sameSliceValue(left.Call.Args[0], right.Call.Args[0])
	}
	return false
}

func sameSliceValue(first, second ssa.Value) bool {
	if first == second {
		return true
	}
	if left, ok := first.(*ssa.Field); ok {
		if right, ok := second.(*ssa.Field); ok && left.Field == right.Field {
			return sameSliceValue(left.X, right.X)
		}
	}
	left, leftLoad := first.(*ssa.UnOp)
	right, rightLoad := second.(*ssa.UnOp)
	if !leftLoad || !rightLoad || left.Op != token.MUL || right.Op != token.MUL || left.Block() != right.Block() {
		return false
	}
	leftAddress, leftKnown := addressOf(left.X)
	rightAddress, rightKnown := addressOf(right.X)
	if !leftKnown || !rightKnown || !sameStorage(leftAddress, rightAddress) {
		return false
	}
	start, end := instructionPoint(left).index, instructionPoint(right).index
	if start > end {
		start, end = end, start
	}
	// Repeated field loads describe the same header only before intervening
	// writes or calls can change it, including through an alias.
	for _, instruction := range left.Block().Instrs[start+1 : end] {
		if _, store := instruction.(*ssa.Store); store {
			return false
		}
		if call, ok := instruction.(ssa.CallInstruction); ok {
			builtin, builtinCall := call.Common().Value.(*ssa.Builtin)
			if !builtinCall || builtin.Name() != "len" && builtin.Name() != "cap" {
				return false
			}
		}
	}
	return true
}
