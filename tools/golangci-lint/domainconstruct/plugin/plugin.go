// Package plugin registers the domain analyzers with golangci-lint's module API.
package plugin

import (
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/diagnostic"
	"github.com/stolostron/ocm-fleet/tools/golangci-lint/domainconstruct"
)

type plugin struct{ analyzer *analysis.Analyzer }

func init() {
	register.Plugin("domainconstruct", func(any) (register.LinterPlugin, error) {
		return plugin{domainconstruct.Analyzer}, nil
	})
	register.Plugin("domainzero", func(any) (register.LinterPlugin, error) {
		return plugin{domainconstruct.ZeroAnalyzer}, nil
	})
}

func (p plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	if p.analyzer != domainconstruct.ZeroAnalyzer {
		return []*analysis.Analyzer{p.analyzer}, nil
	}
	adapter := *p.analyzer
	adapter.Run = func(pass *analysis.Pass) (any, error) {
		copy := *pass
		copy.Report = func(finding analysis.Diagnostic) {
			// golangci-lint flattens Related into independent findings and loses
			// cross-file positions. Keep the native analyzer's structured data,
			// but render it within one finding at this integration boundary.
			pass.Report(diagnostic.WithRelatedLocations(pass.Fset, finding))
		}
		return p.analyzer.Run(&copy)
	}
	return []*analysis.Analyzer{&adapter}, nil
}

func (plugin) GetLoadMode() string { return register.LoadModeTypesInfo }
