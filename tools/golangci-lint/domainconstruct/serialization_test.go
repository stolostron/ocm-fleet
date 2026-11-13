package domainconstruct_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/domainconstruct"
)

func TestSerialization(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), domainconstruct.SerializationAnalyzer,
		"serialization", "serializationpolicy")
}

func TestDecoderInitialization(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), domainconstruct.ZeroAnalyzer, "decodeflow")
}

func TestArrayConstruction(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), domainconstruct.Analyzer, "arrayconstruction")
}
