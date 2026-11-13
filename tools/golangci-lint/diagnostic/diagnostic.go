// Package diagnostic gives analyzers one finding model for native Go tools,
// readable golangci-lint output, and structured audit reports.
package diagnostic

import (
	"encoding/json"
	"fmt"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Certainty describes whether the reported rule violation is known or possible.
type Certainty string

const (
	// Known means the analyzer has established the reported rule violation.
	Known Certainty = "known"
	// Possible means uncertain values or control flow may violate the rule.
	Possible Certainty = "possible"
)

// Finding separates stable identifiers and evidence from its English summary.
// Rule is namespaced by analyzer; Operation and Reason are stable codes.
type Finding struct {
	Rule       string
	Certainty  Certainty
	Operation  string
	Value      string // Source expression, when known; never an SSA temporary name.
	Type       string // Full Go type identity, including the package import path.
	Path       string // Member path through struct fields and collection elements; empty at the root.
	Reason     string // Source or contract behind the violation, independent of certainty.
	Pos        token.Pos
	Message    string
	Provenance []Step // Ordered evidence, including the final use when applicable.
}

// Step records one piece of evidence using a stable kind such as source,
// mutation, conversion, assignment, field, branch, or use.
type Step struct {
	Kind    string
	Pos     token.Pos
	Message string
}

// Diagnostic retains native related locations. Explicit metadata in the message
// also survives hosts that discard Category, including golangci-lint.
func (finding Finding) Diagnostic() analysis.Diagnostic {
	var message strings.Builder
	message.WriteString(finding.Message)
	fmt.Fprintf(&message, "\n  | rule: %s; certainty: %s; reason: %s", finding.Rule, finding.Certainty, finding.Reason)
	fmt.Fprintf(&message, "\n  | operation: %s; value: %s; type: %s; path: %s",
		finding.Operation, quote(finding.Value), quote(finding.Type), quote(finding.Path))
	result := analysis.Diagnostic{Pos: finding.Pos, Category: finding.Rule, Message: message.String()}
	for _, step := range finding.Provenance {
		result.Related = append(result.Related, analysis.RelatedInformation{
			Pos: step.Pos, Message: step.Kind + ": " + step.Message,
		})
	}
	return result
}

// WithRelatedLocations keeps provenance within its finding for hosts that would
// otherwise turn related information into separate issues. Pipe delimiters
// preserve record boundaries when an editor collapses whitespace.
func WithRelatedLocations(fset *token.FileSet, finding analysis.Diagnostic) analysis.Diagnostic {
	var message strings.Builder
	message.WriteString(finding.Message)
	for _, related := range finding.Related {
		kind, detail, found := strings.Cut(related.Message, ": ")
		if !found {
			kind, detail = "related", related.Message
		}
		position := fset.PositionFor(related.Pos, true)
		fmt.Fprintf(&message, "\n  | %s: %s (%s, line %d, column %d)",
			kind, escape(detail), quote(position.Filename), position.Line, position.Column)
	}
	finding.Message, finding.Related = message.String(), nil
	return finding
}

func quote(value string) string {
	var encoded strings.Builder
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value) // Encoding a string to strings.Builder cannot fail.
	// A pipe inside a quoted expression must not become a record delimiter.
	return strings.ReplaceAll(strings.TrimSuffix(encoded.String(), "\n"), "|", `\u007c`)
}

func escape(value string) string {
	return strings.NewReplacer(`\`, `\\`, "|", `\|`, "\n", `\n`, "\r", `\r`).Replace(value)
}
