package main

import (
	"go/ast"
	"go/types"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/analysis/passes/modernize"
	"golang.org/x/tools/go/ast/inspector"
)

func init() {
	register.Plugin("mapcopy", func(any) (register.LinterPlugin, error) {
		return mapCopyPlugin{}, nil
	})
}

type mapCopyPlugin struct{}

func (mapCopyPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	analyzer := *modernize.MapsLoopAnalyzer
	analyzer.Run = runMapCopy
	return []*analysis.Analyzer{&analyzer}, nil
}

func (mapCopyPlugin) GetLoadMode() string {
	return register.LoadModeTypesInfo
}

func runMapCopy(pass *analysis.Pass) (any, error) {
	root := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector).Root()
	filtered := *pass
	filtered.Report = func(diagnostic analysis.Diagnostic) {
		cursor, ok := root.FindByPos(diagnostic.Pos, diagnostic.End)
		if !ok {
			return
		}
		index, ok := cursor.Node().(*ast.IndexExpr)
		if !ok || !isMapIdentifier(pass, index.X) {
			return
		}

		// A call can move evaluation of the destination before the source and
		// evaluate it even on empty input. Only copy between map identifiers.
		// An iterator could rebind the destination between yields.
		for enclosing := range cursor.Enclosing((*ast.RangeStmt)(nil)) {
			if isMapIdentifier(pass, enclosing.Node().(*ast.RangeStmt).X) {
				pass.Report(diagnostic)
			}
			return
		}
	}
	return modernize.MapsLoopAnalyzer.Run(&filtered)
}

func isMapIdentifier(pass *analysis.Pass, expression ast.Expr) bool {
	ident, ok := ast.Unparen(expression).(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = pass.TypesInfo.TypeOf(ident).Underlying().(*types.Map)
	return ok
}
