// Package domainaccess checks the domain model's field encapsulation boundary.
package domainaccess

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer reports access to private domain fields outside their owner's methods.
var Analyzer = &analysis.Analyzer{
	Name: "domainaccess",
	Doc:  "use domain getters outside a type's own methods",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	if pass.Pkg.Path() != "github.com/stolostron/ocm-fleet/server/internal/domain" {
		return nil, nil
	}
	for _, file := range pass.Files {
		// These files are the documented raw-state serialization boundaries.
		switch filepath.Base(pass.Fset.Position(file.Pos()).Filename) {
		case "snapshot.go", "etag.go":
			continue
		}
		for _, declaration := range file.Decls {
			function, _ := declaration.(*ast.FuncDecl)
			ast.Inspect(declaration, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				selection := pass.TypesInfo.Selections[selector]
				if selection == nil || selection.Kind() != types.FieldVal {
					return true
				}
				field := selection.Obj().(*types.Var)
				owner := fieldOwner(selection)
				if field.Exported() || owner == nil || owner.Obj().Pkg() != pass.Pkg || ownsAccess(pass, function, owner) {
					return true
				}
				if getter := getterFor(owner, field); getter != "" {
					pass.Reportf(selector.Pos(), "access %s.%s through %s methods; use %s() for reads", owner.Obj().Name(), field.Name(), owner.Obj().Name(), getter)
				}
				return true
			})
		}
	}
	return nil, nil
}

// Promoted fields belong to their declaring embedded type, not the outer type.
func fieldOwner(selection *types.Selection) *types.Named {
	current := selection.Recv()
	for depth, index := range selection.Index() {
		owner := namedType(current)
		if owner == nil {
			return nil
		}
		if depth == len(selection.Index())-1 {
			return owner
		}
		current = owner.Underlying().(*types.Struct).Field(index).Type()
	}
	return nil
}

func ownsAccess(pass *analysis.Pass, function *ast.FuncDecl, owner *types.Named) bool {
	if function == nil {
		return false
	}
	signature := pass.TypesInfo.Defs[function.Name].Type().(*types.Signature)
	if receiver := signature.Recv(); receiver != nil {
		return namedType(receiver.Type()).Origin() == owner.Origin()
	}
	// A constructor exception applies only to the type it actually constructs.
	if function.Name.Name == "New"+owner.Obj().Name() {
		for result := range signature.Results().Len() {
			if constructed := namedType(signature.Results().At(result).Type()); constructed != nil && constructed.Origin() == owner.Origin() {
				return true
			}
		}
	}
	return false
}

// Start with unambiguous getters. Fields without a corresponding exported,
// zero-argument, single-result method remain outside this analyzer's scope.
func getterFor(owner *types.Named, field *types.Var) string {
	methods := types.NewMethodSet(types.NewPointer(owner))
	for index := range methods.Len() {
		method := methods.At(index).Obj().(*types.Func)
		signature := method.Type().(*types.Signature)
		if method.Exported() && strings.EqualFold(method.Name(), field.Name()) && signature.Params().Len() == 0 && signature.Results().Len() == 1 && types.Identical(signature.Results().At(0).Type(), field.Type()) {
			return method.Name()
		}
	}
	return ""
}

func namedType(value types.Type) *types.Named {
	value = types.Unalias(value)
	if pointer, ok := value.(*types.Pointer); ok {
		value = types.Unalias(pointer.Elem())
	}
	named, _ := value.(*types.Named)
	return named
}
