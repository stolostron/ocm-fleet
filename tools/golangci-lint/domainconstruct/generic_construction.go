package domainconstruct

import (
	"go/ast"
	"go/types"
)

func (c *checker) checkGenericConstruction(call *ast.CallExpr, ancestors []ast.Node) bool {
	function, ok := expressionObject(c.pass.TypesInfo, call.Fun).(*types.Func)
	if !ok {
		return true
	}
	fact := c.contracts.functions[function.Origin()]
	if fact == nil {
		return true
	}
	for _, result := range fact.Sources {
		target := c.pass.TypesInfo.TypeOf(call)
		if tuple, ok := target.(*types.Tuple); ok {
			target = tuple.At(result.Result).Type()
		}
		if !c.checkGenericSource(result.Value, target, call, function, ancestors) {
			return false
		}
	}
	return true
}

func (c *checker) checkGenericSource(source valueSource, target types.Type, call *ast.CallExpr, function *types.Func, ancestors []ast.Node) bool {
	if source.Kind == sourceConversion {
		if p := c.policy(target); p != nil && !c.approved(p, ancestors) && !constructorCall(p, target, function) {
			c.reportConstruction(call, p)
			return false
		}
	}
	for index, child := range source.Values {
		childType := target
		switch source.Kind {
		case sourceFields:
			if structure, ok := target.Underlying().(*types.Struct); ok && index < structure.NumFields() {
				childType = structure.Field(index).Type()
			}
		case sourcePointer:
			if pointer, ok := target.Underlying().(*types.Pointer); ok {
				childType = pointer.Elem()
			}
		}
		if !c.checkGenericSource(child, childType, call, function, ancestors) {
			return false
		}
	}
	return true
}

func constructorCall(p *policy, target types.Type, function *types.Func) bool {
	named := domainType(target)
	if named == nil || function.Pkg() != named.Obj().Pkg() {
		return false
	}
	name := function.Name()
	if receiver := function.Type().(*types.Signature).Recv(); receiver != nil {
		typeName := receiver.Type()
		if pointer, ok := typeName.(*types.Pointer); ok {
			typeName = pointer.Elem()
		}
		if named, ok := typeName.(*types.Named); ok {
			name = named.Obj().Name() + "." + name
		}
	}
	for _, constructor := range p.fact.Constructors {
		if name == constructor {
			return true
		}
	}
	return false
}
