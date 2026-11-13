package domainconstruct_test

import (
	"go/ast"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// Labels describe the source use rather than compiler-generated slice packing.
// Paths may mix struct fields and collection elements; neither is called a
// field unless it actually denotes a struct field.
func checkUseLabels(t *testing.T, result *analysistest.Result) {
	t.Helper()
	type label struct {
		operation string
		path      string
		summary   string
	}
	argument := label{"argument", "", "argument is an empty zeroflow.ID; empty values are invalid"}
	receiver := label{"receiver", "", "receiver is a zero zeroflow.Ref; zero values are invalid"}
	variadic := label{"variadic-argument", "", "variadic argument is an empty zeroflow.ID; empty values are invalid"}
	nested := label{"argument", "ID", "argument contains an empty zeroflow.ID at path ID; empty values are invalid"}
	collection := label{"argument", "[0]", "argument contains an empty zeroflow.ID at path [0]; empty values are invalid"}
	wants := map[string]label{
		"PositionArgument":                 argument,
		"PositionPointerArgument":          nested,
		"PositionMultilineArgument":        argument,
		"PositionRepeatedArgument":         argument,
		"PositionMethodReceiver":           receiver,
		"PositionPointerReceiver":          receiver,
		"PositionMethodArgument":           argument,
		"PositionMethodExpression":         argument,
		"PositionMethodExpressionReceiver": receiver,
		"PositionInterfaceReceiver":        receiver,
		"PositionInterfaceArgument":        argument,
		"PositionBoundMethodArgument":      argument,
		"PositionBoundMethodReceiver":      receiver,
		"PositionVariadicArgument":         variadic,
		"PositionEllipsisArgument": {
			"argument", "[1]", "argument contains an empty zeroflow.ID at path [1]; empty values are invalid",
		},
		"PositionVariadicMethodArgument":   variadic,
		"PositionVariadicMethodExpression": variadic,
		"PositionAppendArgument": {
			"append", "", "append argument is an empty zeroflow.ID; empty values are invalid",
		},
		"PositionCopyArgument": {
			"copy", "[1]", "copy source contains an empty zeroflow.ID at path [1]; empty values are invalid",
		},
		"PositionClearArgument": {
			"clear", "", "slice element after clear is an empty zeroflow.ID; empty values are invalid",
		},
		"PositionPackageArgument":       variadic,
		"PositionGenericArgument":       argument,
		"PositionParenthesizedFunction": argument,
		"PositionTupleArgument": {
			"argument", "", "argument may be an empty zeroflow.ID; empty values are invalid",
		},
		"PositionDeferArgument":  argument,
		"PositionGoArgument":     argument,
		"PositionStructArgument": nested,
		"PositionNestedCollectionArgument": {
			"argument", "Reports[0].ID", "argument contains an empty zeroflow.ID at path Reports[0].ID; empty values are invalid",
		},
		"PositionSliceArgument": collection,
		"PositionArrayArgument": collection,
		"PositionVariadicNestedArgument": {
			"variadic-argument", "ID", "variadic argument contains an empty zeroflow.ID at path ID; empty values are invalid",
		},
		"PositionVariadicCollectionArgument": {
			"variadic-argument", "[0]", "variadic argument contains an empty zeroflow.ID at path [0]; empty values are invalid",
		},
		"PositionNestedEllipsisArgument": {
			"argument", "[0].ID", "argument contains an empty zeroflow.ID at path [0].ID; empty values are invalid",
		},
		"PositionPossibleVariadicArgument": {
			"variadic-argument", "", "variadic argument may be an empty zeroflow.ID; empty values are invalid",
		},
		"PositionGenericVariadicArgument": variadic,
		"PositionShadowedBuiltin":         argument,
		"PositionAppendEllipsis": {
			"append", "[0]", "append argument contains an empty zeroflow.ID at path [0]; empty values are invalid",
		},
		"PositionArrayReceiver": {
			"receiver", "[0]", "receiver contains an empty zeroflow.ID at path [0]; empty values are invalid",
		},
		"LabelReturn": {
			"return", "", "return produces an empty zeroflow.ID; empty values are invalid",
		},
		"LabelReturnStruct": {
			"return", "ID", "return contains an empty zeroflow.ID at path ID; empty values are invalid",
		},
		"LabelReturnCollection": {
			"return", "[0]", "return contains an empty zeroflow.ID at path [0]; empty values are invalid",
		},
		"LabelMapKey": {
			"map-key", "", "map key is an empty zeroflow.ID; empty values are invalid",
		},
		"LabelMapValue": {
			"map-value", "", "map value is an empty zeroflow.ID; empty values are invalid",
		},
		"LabelMapCollectionValue": {
			"map-value", "[0]", "map value contains an empty zeroflow.ID at path [0]; empty values are invalid",
		},
		"LabelSend": {
			"send", "", "channel send uses an empty zeroflow.ID; empty values are invalid",
		},
		"LabelSelectSend": {
			"send", "", "channel send uses an empty zeroflow.ID; empty values are invalid",
		},
		"LabelAssignment": {
			"assignment", "", "assignment stores an empty zeroflow.ID; empty values are invalid",
		},
		"LabelConversion": {
			"conversion", "", "conversion uses an empty zeroflow.ID; empty values are invalid",
		},
		"LabelOperand": {
			"operand", "", "operand is an empty zeroflow.ID; empty values are invalid",
		},
		"LabelClearNested": {
			"clear", "ID", "slice element after clear contains an empty zeroflow.ID at path ID; empty values are invalid",
		},
	}
	for _, file := range result.Pass.Files {
		filename := result.Pass.Fset.Position(file.Pos()).Filename
		if !strings.HasSuffix(filename, "/diagnostic_positions.go") &&
			!strings.HasSuffix(filename, "/diagnostic_labels.go") {
			continue
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || (!strings.HasPrefix(function.Name.Name, "Position") && !strings.HasPrefix(function.Name.Name, "Label")) {
				continue
			}
			want, ok := wants[function.Name.Name]
			if !ok {
				t.Errorf("%s has no label expectation", function.Name.Name)
				continue
			}
			found := false
			for _, finding := range result.Diagnostics {
				if finding.Pos < function.Pos() || finding.Pos > function.End() {
					continue
				}
				found = true
				summary, _, _ := strings.Cut(finding.Message, "\n")
				if summary != want.summary {
					t.Errorf("%s summary = %q, want %q", function.Name.Name, summary, want.summary)
				}
				if !strings.Contains(finding.Message, "| operation: "+want.operation+";") ||
					!strings.Contains(finding.Message, "; path: "+strconv.Quote(want.path)) {
					t.Errorf("%s wants operation %q and path %q: %s",
						function.Name.Name, want.operation, want.path, finding.Message)
				}
				useFound := false
				for _, step := range finding.Related {
					if step.Message == "use: "+want.operation && step.Pos == finding.Pos {
						useFound = true
					}
				}
				if !useFound {
					t.Errorf("%s has no matching use label and position in provenance", function.Name.Name)
				}
			}
			if !found {
				t.Errorf("%s has no diagnostic to check", function.Name.Name)
			}
		}
	}
}
