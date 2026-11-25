package domainconstruct

import "go/types"

// The shared Optional owns the relationship between its private value and
// presence flag. Its absent zero is valid, but Some still consumes a valid T.
// Recognition follows declaration identity, including aliases and generic
// instances; another type with the same name or representation has no contract.
func optionalPayload(t types.Type) types.Type {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.TypeArgs().Len() != 1 {
		return nil
	}
	object := named.Origin().Obj()
	if !optionalDeclaration(object) {
		return nil
	}
	return named.TypeArgs().At(0)
}

func optionalGet(function *types.Func) bool {
	if function == nil || function.Name() != "Get" {
		return false
	}
	receiver := function.Origin().Type().(*types.Signature).Recv()
	if receiver == nil {
		return false
	}
	named := domainType(receiver.Type())
	return named != nil && optionalDeclaration(named.Obj())
}

func optionalDeclaration(object *types.TypeName) bool {
	return object.Pkg() != nil && object.Pkg().Path() == "github.com/stolostron/ocm-fleet/server/pkg/optional" && object.Name() == "Optional"
}
