package domainconstruct

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
)

// Check straight-line alias bindings and retain them on entry to a bare block
// or if branch. Rebinding affects only that path. Drop the outer bindings at
// the join rather than infer which assignments ran.
func (c *checker) checkBlock(block *ast.BlockStmt, ancestors []ast.Node) {
	outer := c.storageAliases
	c.storageAliases = maps.Clone(outer)
	defer func() { c.storageAliases = outer }()
	ancestors = append(ancestors, block)
	for _, statement := range block.List {
		switch statement := statement.(type) {
		case *ast.BlockStmt, *ast.IfStmt:
			c.walk(statement, ancestors)
			clear(c.storageAliases)
			continue
		case *ast.DeclStmt:
			if declaration, ok := statement.Decl.(*ast.GenDecl); ok && declaration.Tok == token.VAR {
				declarationAncestors := append(ancestors, statement, declaration)
				for _, spec := range declaration.Specs {
					spec := spec.(*ast.ValueSpec)
					c.walk(spec, declarationAncestors)
					c.bindLocalStorage(len(spec.Names), func(index int) ast.Expr { return spec.Names[index] }, spec.Values, declarationAncestors)
				}
				continue
			}
		case *ast.AssignStmt, *ast.ExprStmt, *ast.IncDecStmt, *ast.EmptyStmt,
			*ast.ReturnStmt, *ast.SendStmt, *ast.DeferStmt, *ast.GoStmt:
		default:
			clear(c.storageAliases)
		}
		c.walkStorageStatement(statement, ancestors)
	}
}

func (c *checker) checkIf(statement *ast.IfStmt, ancestors []ast.Node) {
	outer := c.storageAliases
	c.storageAliases = maps.Clone(outer)
	defer func() { c.storageAliases = outer }()
	ancestors = append(ancestors, statement)
	if statement.Init != nil {
		c.walkStorageStatement(statement.Init, ancestors)
	}
	c.walk(statement.Cond, ancestors)
	c.walk(statement.Body, ancestors)
	if statement.Else != nil {
		c.walk(statement.Else, ancestors)
	}
}

func (c *checker) walkStorageStatement(statement ast.Stmt, ancestors []ast.Node) {
	c.walk(statement, ancestors)
	if assignment, ok := statement.(*ast.AssignStmt); ok && (assignment.Tok == token.ASSIGN || assignment.Tok == token.DEFINE) {
		c.bindLocalStorage(len(assignment.Lhs), func(index int) ast.Expr { return assignment.Lhs[index] }, assignment.Rhs, ancestors)
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
		if owner != nil || c.storageAliases[variable] != nil {
			updates = append(updates, binding{variable, owner})
		}
	}
	// Simultaneous assignments take all their sources from the previous state.
	for _, update := range updates {
		if update.owner == nil {
			delete(c.storageAliases, update.variable)
			continue
		}
		if c.storageAliases == nil {
			c.storageAliases = make(map[*types.Var]*policy)
		}
		c.storageAliases[update.variable] = update.owner
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

// A conversion between slice types or map types preserves the storage.
func (c *checker) storageConversionSource(call *ast.CallExpr) ast.Expr {
	info := c.pass.TypesInfo
	if len(call.Args) != 1 || !info.Types[call.Fun].IsType() {
		return nil
	}
	source := operandUnderlying(info.TypeOf(call.Args[0]))
	switch source.(type) {
	case *types.Slice, *types.Map:
		if types.IdenticalIgnoreTags(source, operandUnderlying(info.TypeOf(call))) {
			return call.Args[0]
		}
	}
	return nil
}
