package domainconstruct

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

type boxedObservation struct {
	value ssa.Value
	point flowPoint
}

type boxedVisit struct {
	observation boxedObservation
	path        string
}

type boxedWalk struct {
	storage *zeroChecker
	active  map[boxedObservation]bool
	seen    map[boxedVisit]bool
}

// Source types cover generic parameters and return payloads. Local SSA retains
// concrete values boxed into any, including variadic calls, without tracing
// arbitrary byte buffers or performing whole-program pointer analysis.
func (c *serializationChecker) checkBoxedCalls() {
	functions := c.pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA).SrcFuncs
	storage := &zeroChecker{pass: c.pass, policies: c.policies, contracts: c.contracts, locating: make(map[ssa.Value]bool), work: new(workBudget)}
	storage.indexStores(functions)
	for _, function := range functions {
		for _, block := range function.Blocks {
			for instructionIndex, instruction := range block.Instrs {
				call, ok := instruction.(ssa.CallInstruction)
				if !ok {
					continue
				}
				node := c.calls[call.Common().Pos()]
				if node == nil {
					continue
				}
				object := callObject(call.Common())
				c.callPosition = node.Lparen
				for _, boundary := range c.contracts.boundariesFor(object, c.policies) {
					for _, index := range boundary.Results {
						if index < call.Common().Signature().Results().Len() {
							c.checkType(encodingOperand{node: node, codec: boundary.Codec, operation: "result"}, call.Common().Signature().Results().At(index).Type())
						}
					}
					if callee := call.Common().StaticCallee(); callee != nil {
						arguments := callee.TypeArgs()
						for _, index := range boundary.TypeParameters {
							if index < len(arguments) {
								c.checkType(encodingOperand{node: node, codec: boundary.Codec, operation: "type-argument"}, arguments[index])
							}
						}
					}
					for _, selection := range []struct {
						indices   []int
						operation string
					}{{boundary.Encode, "encode"}, {boundary.Decode, "decode"}} {
						for _, index := range selection.indices {
							arguments := c.arguments(node, object, index)
							var source ast.Node = node
							if len(arguments) != 0 {
								source = arguments[0]
							}
							storage.resetWork()
							operand := encodingOperand{node: source, codec: boundary.Codec, operation: selection.operation}
							walk := boxedWalk{storage: storage, active: make(map[boxedObservation]bool), seen: make(map[boxedVisit]bool)}
							c.checkBoxedValue(contractArgument(call.Common(), index), operand, flowPoint{block, instructionIndex}, &walk)
						}
					}
				}
			}
		}
	}
	c.callPosition = token.NoPos
}

func (c *serializationChecker) checkBoxedValue(value ssa.Value, operand encodingOperand, point flowPoint, walk *boxedWalk) {
	observation := boxedObservation{value, point}
	visit := boxedVisit{observation, operand.path}
	storage := walk.storage
	if value == nil || walk.seen[visit] || walk.active[observation] || !storage.work.take() {
		return
	}
	// Reuse work on the same path. While visiting a value, stop cycles even
	// when following a recursive field would extend the displayed path.
	walk.seen[visit] = true
	walk.active[observation] = true
	defer delete(walk.active, observation)
	c.checkType(operand, value.Type())
	if c.policies.lookup(value.Type()) != nil {
		return // The declared root owns its entire encoded representation.
	}
	switch value := value.(type) {
	case *ssa.MakeInterface:
		c.checkBoxedValue(value.X, operand, point, walk)
		return
	case *ssa.ChangeInterface:
		c.checkBoxedValue(value.X, operand, point, walk)
		return
	case *ssa.Phi:
		for _, edge := range value.Edges {
			c.checkBoxedValue(edge, operand, point, walk)
		}
		return
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if location, known := storage.collectionAddress(value.X); known {
				c.checkBoxedStorage(location, value.Type(), operand, instructionPoint(value), point, walk)
			}
		}
		return
	}
	if pointer, ok := value.Type().Underlying().(*types.Pointer); ok {
		if location, known := storage.collectionAddress(value); known {
			c.checkBoxedStorage(location, pointer.Elem(), operand, point, point, walk)
		}
		return
	}
	if view, known := storage.collectionOf(value); known {
		c.checkBoxedCollection(view, operand, point, point, walk)
	}
}

func (c *serializationChecker) checkBoxedCollection(view collectionView, operand encodingOperand, point, use flowPoint, walk *boxedWalk) {
	storage := walk.storage
	visited := make(map[int]bool)
	for _, write := range storage.stores[view.storage.root] {
		if !storage.work.take() {
			return
		}
		if !pathPrefix(write.target.path, view.storage.path) || len(write.target.path) <= len(view.storage.path) {
			continue
		}
		index := write.target.path[len(view.storage.path)]
		if visited[index] || int64(index) < view.start || view.end >= 0 && int64(index) >= view.end {
			continue
		}
		visited[index] = true
		child := address{root: view.storage.root, path: append(slices.Clone(view.storage.path), index)}
		c.checkBoxedStorage(child, view.element, operand, point, use, walk)
	}
}

func (c *serializationChecker) checkBoxedStorage(location address, t types.Type, operand encodingOperand, point, use flowPoint, walk *boxedWalk) {
	storage := walk.storage
	if c.policies.lookup(t) != nil || standardPlainType(t) || !storage.work.take() {
		return
	}
	switch shape := t.Underlying().(type) {
	case *types.Struct:
		for selected := range c.jsonShape.fields(shape, operand.codec == "json") {
			if operand.codec != "json" && !selected.field.Exported() {
				continue
			}
			if child, read, known := storage.fieldStorage(location, shape, selected, point, use); known {
				field := operand
				field.path = fieldPath(operand.path, selected.path)
				c.checkBoxedStorage(child, selected.field.Type(), field, read, use, walk)
			}
		}
	case *types.Array:
		view := collectionView{storage: location, element: shape.Elem(), end: shape.Len(), capEnd: shape.Len()}
		c.checkBoxedCollection(view, operand, point, use, walk)
	default:
		if value := storage.storedReference(location, point); value != nil {
			c.checkBoxedValue(value, operand, use, walk)
		}
	}
}
