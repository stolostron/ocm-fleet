// Package plugin adapts OCM Fleet's Go analyzers to golangci-lint's module API.
package plugin

import (
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/domainaccess"
)

func init() {
	register.Plugin("fleetdomain", func(any) (register.LinterPlugin, error) {
		return domainPlugin{}, nil
	})
}

type domainPlugin struct{}

func (domainPlugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{domainaccess.Analyzer}, nil
}

func (domainPlugin) GetLoadMode() string { return register.LoadModeTypesInfo }
