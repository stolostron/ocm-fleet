package domainconstruct

import (
	"go/ast"
	"go/token"
	"go/types"
)

type storageAliasScope struct {
	owners  map[*types.Var]*policy
	stopped bool
}

func (scope *storageAliasScope) stop() {
	clear(scope.owners)
	scope.stopped = true
}

// Alias facts describe straight-line bindings within one block. Start new
// facts at nested blocks and discard prior bindings at control flow statements.
func (c *checker) checkBlock(block *ast.BlockStmt, ancestors []ast.Node) {
	outer := c.storageAliases
	c.storageAliases = storageAliasScope{}
	defer func() { c.storageAliases = outer }()
	ancestors = append(ancestors, block)
	for _, statement := range block.List {
		switch statement.(type) {
		case *ast.AssignStmt, *ast.DeclStmt, *ast.ExprStmt, *ast.IncDecStmt, *ast.EmptyStmt,
			*ast.ReturnStmt, *ast.SendStmt, *ast.DeferStmt, *ast.GoStmt:
		default:
			clear(c.storageAliases.owners)
		}
		c.walk(statement, ancestors)
		if c.storageAliases.stopped {
			continue
		}
		switch statement := statement.(type) {
		case *ast.AssignStmt:
			if statement.Tok == token.ASSIGN || statement.Tok == token.DEFINE {
				c.bindLocalStorage(len(statement.Lhs), func(index int) ast.Expr { return statement.Lhs[index] }, statement.Rhs, ancestors)
			}
		case *ast.DeclStmt:
			if declaration, ok := statement.Decl.(*ast.GenDecl); ok && declaration.Tok == token.VAR {
				for _, spec := range declaration.Specs {
					spec := spec.(*ast.ValueSpec)
					c.bindLocalStorage(len(spec.Names), func(index int) ast.Expr { return spec.Names[index] }, spec.Values, ancestors)
				}
			}
		}
	}
}

func (c *checker) bindLocalStorage(count int, target func(int) ast.Expr, sources []ast.Expr, ancestors []ast.Node) {
	type binding struct {
		variable *types.Var
		owner    *policy
	}
	var updates []binding
	for index := range count {
		identifier, ok := ast.Unparen(target(index)).(*ast.Ident)
		if !ok {
			continue
		}
		variable := c.localStorageVariable(identifier)
		if variable == nil {
			continue
		}
		var owner *policy
		if len(sources) == count {
			if base := c.mutableStorageBase(sources[index]); base != nil {
				owner = c.unapprovedOwner(base, ancestors)
			}
		}
		if owner != nil || c.storageAliases.owners[variable] != nil {
			updates = append(updates, binding{variable, owner})
		}
	}
	// Simultaneous assignments take all their sources from the previous state.
	for _, update := range updates {
		if update.owner == nil {
			delete(c.storageAliases.owners, update.variable)
			continue
		}
		if c.storageAliases.owners == nil {
			c.storageAliases.owners = make(map[*types.Var]*policy)
		}
		c.storageAliases.owners[update.variable] = update.owner
	}
}

func (c *checker) localStorageVariable(identifier *ast.Ident) *types.Var {
	variable, ok := c.pass.TypesInfo.ObjectOf(identifier).(*types.Var)
	if !ok || identifier.Name == "_" || variable.Parent() == c.pass.Pkg.Scope() {
		return nil
	}
	switch operandUnderlying(variable.Type()).(type) {
	case *types.Slice, *types.Map:
		return variable
	}
	return nil
}
