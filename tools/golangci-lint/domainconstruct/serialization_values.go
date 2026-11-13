package domainconstruct

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

// Source types cover generic parameters and return payloads. Local SSA retains
// concrete values boxed into any, including variadic calls, without tracing
// arbitrary byte buffers or performing whole-program pointer analysis.
func (c *serializationChecker) checkBoxedCalls() {
	functions := c.pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA).SrcFuncs
	storage := &zeroChecker{pass: c.pass, policies: c.policies, contracts: c.contracts, locating: make(map[ssa.Value]bool)}
	storage.indexStores(functions)
	for _, function := range functions {
		for _, block := range function.Blocks {
			for _, instruction := range block.Instrs {
				call, ok := instruction.(ssa.CallInstruction)
				if !ok {
					continue
				}
				node := c.calls[call.Common().Pos()]
				if node == nil {
					continue
				}
				object := serializationCallObject(call.Common())
				c.callPosition = node.Lparen
				for _, boundary := range c.contracts.boundariesFor(object, c.policies) {
					for _, index := range boundary.Results {
						if index < call.Common().Signature().Results().Len() {
							c.checkType(node, call.Common().Signature().Results().At(index).Type(), boundary.Codec, "result")
						}
					}
					if callee := call.Common().StaticCallee(); callee != nil {
						arguments := callee.TypeArgs()
						for _, index := range boundary.TypeParameters {
							if index < len(arguments) {
								c.checkType(node, arguments[index], boundary.Codec, "type-argument")
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
							c.checkBoxedValue(serializationArgument(call.Common(), index), source, boundary.Codec, selection.operation,
								instructionPoint(instruction), storage, make(map[ssa.Value]bool))
						}
					}
				}
			}
		}
	}
	c.callPosition = token.NoPos
}

func (c *serializationChecker) checkBoxedValue(value ssa.Value, source ast.Node, codec, operation string, point flowPoint, storage *zeroChecker, seen map[ssa.Value]bool) {
	if value == nil || seen[value] {
		return
	}
	seen[value] = true
	defer delete(seen, value)
	c.checkType(source, value.Type(), codec, operation)
	if c.policies.lookup(value.Type()) != nil {
		return // The declared root owns its entire encoded representation.
	}
	switch value := value.(type) {
	case *ssa.MakeInterface:
		c.checkBoxedValue(value.X, source, codec, operation, point, storage, seen)
		return
	case *ssa.ChangeInterface:
		c.checkBoxedValue(value.X, source, codec, operation, point, storage, seen)
		return
	case *ssa.Phi:
		for _, edge := range value.Edges {
			c.checkBoxedValue(edge, source, codec, operation, point, storage, seen)
		}
		return
	case *ssa.UnOp:
		if value.Op == token.MUL {
			if location, known := storage.collectionAddress(value.X); known {
				c.checkBoxedStorage(location, value.Type(), source, codec, operation, instructionPoint(value), storage, seen)
			}
		}
		return
	}
	if pointer, ok := value.Type().Underlying().(*types.Pointer); ok {
		if location, known := storage.collectionAddress(value); known {
			c.checkBoxedStorage(location, pointer.Elem(), source, codec, operation, point, storage, seen)
		}
		return
	}
	if view, known := storage.collectionOf(value); known {
		visited := make(map[string]bool)
		for _, write := range storage.stores[view.storage.root] {
			if !pathPrefix(write.target.path, view.storage.path) || len(write.target.path) <= len(view.storage.path) {
				continue
			}
			index := write.target.path[len(view.storage.path)]
			if int64(index) < view.start || view.end >= 0 && int64(index) >= view.end {
				continue
			}
			path := append(slices.Clone(view.storage.path), index)
			if visited[pathKey(path)] {
				continue
			}
			visited[pathKey(path)] = true
			c.checkBoxedStorage(address{root: view.storage.root, path: path}, view.element, source, codec, operation, point, storage, seen)
		}
	}
}

func (c *serializationChecker) checkBoxedStorage(location address, t types.Type, source ast.Node, codec, operation string, point flowPoint, storage *zeroChecker, seen map[ssa.Value]bool) {
	if c.policies.lookup(t) != nil || standardPlainType(t) {
		return
	}
	if structure, ok := t.Underlying().(*types.Struct); ok {
		for index := range structure.NumFields() {
			field := structure.Field(index)
			if !field.Exported() || codec == "json" && strings.Split(reflect.StructTag(structure.Tag(index)).Get("json"), ",")[0] == "-" {
				continue
			}
			child := address{root: location.root, path: append(slices.Clone(location.path), index)}
			c.checkBoxedStorage(child, field.Type(), source, codec, operation, point, storage, seen)
		}
		return
	}
	if value := storage.storedReference(location, point, make(map[memoryVisit]bool)); value != nil {
		c.checkBoxedValue(value, source, codec, operation, point, storage, seen)
	}
}
