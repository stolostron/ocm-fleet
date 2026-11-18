package domainconstruct_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/domainconstruct"
)

func TestErrorJoinFlow(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), domainconstruct.ZeroAnalyzer, "errorjoins")
}
