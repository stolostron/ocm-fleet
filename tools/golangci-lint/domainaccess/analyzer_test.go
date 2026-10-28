package domainaccess_test

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/domainaccess"
)

func TestDomainAccess(t *testing.T) {
	analysistest.Run(t, filepath.Join("testdata", "domain"), domainaccess.Analyzer, "./...")
}
