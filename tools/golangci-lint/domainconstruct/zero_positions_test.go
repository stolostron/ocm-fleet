package domainconstruct_test

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func checkUsePositions(t *testing.T, result *analysistest.Result) {
	t.Helper()
	for _, file := range result.Pass.Files {
		if !strings.HasSuffix(result.Pass.Fset.Position(file.Pos()).Filename, "/diagnostic_positions.go") {
			continue
		}
		// Each marker identifies the source expression the diagnostic should
		// highlight, independently of the SSA instruction that consumes it.
		positions := make(map[token.Pos]bool)
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if comment.Text != "/* position */" {
					continue
				}
				var next token.Pos
				ast.Inspect(file, func(node ast.Node) bool {
					if node == nil || node.End() <= comment.End() {
						return false
					}
					if expression, ok := node.(ast.Expr); ok && expression.Pos() >= comment.End() &&
						(!next.IsValid() || expression.Pos() < next) {
						next = expression.Pos()
					}
					return true
				})
				if !next.IsValid() {
					t.Fatalf("position marker has no following expression: %s", result.Pass.Fset.Position(comment.Pos()))
				}
				positions[next] = false
			}
		}
		if len(positions) == 0 {
			t.Fatal("position fixture has no marked expressions")
		}
		for _, finding := range result.Diagnostics {
			if finding.Pos < file.Pos() || finding.Pos > file.End() {
				continue
			}
			if _, expected := positions[finding.Pos]; !expected {
				t.Errorf("diagnostic does not start at a marked expression: %s: %s",
					result.Pass.Fset.Position(finding.Pos), finding.Message)
				continue
			}
			positions[finding.Pos] = true
			for _, step := range finding.Related {
				if strings.HasPrefix(step.Message, "use: ") && step.Pos != finding.Pos {
					t.Errorf("use provenance at %s differs from diagnostic at %s",
						result.Pass.Fset.Position(step.Pos), result.Pass.Fset.Position(finding.Pos))
				}
			}
		}
		for pos, found := range positions {
			if !found {
				t.Errorf("no diagnostic starts at the marked expression: %s", result.Pass.Fset.Position(pos))
			}
		}
	}
}
