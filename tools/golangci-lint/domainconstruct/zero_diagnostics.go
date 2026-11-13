package domainconstruct

import (
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/diagnostic"
)

// zeroReason identifies the zero source or missing validity contract. Certainty
// and provenance describe how branches and unknown effects affect that evidence.
type zeroReason string

const (
	reasonZeroValue         zeroReason = "zero-value"
	reasonInputMayBeZero    zeroReason = "input-may-be-zero"
	reasonSourcePermitsZero zeroReason = "source-permits-zero"
	reasonConversion        zeroReason = "conversion-may-produce-zero"
	reasonUncheckedError    zeroReason = "unchecked-error"
	reasonUncheckedLookup   zeroReason = "unchecked-map-lookup"
	reasonUncheckedReceive  zeroReason = "unchecked-receive"
	reasonUncheckedAssert   zeroReason = "unchecked-assertion"
	reasonZeroStorage       zeroReason = "zero-initialized-storage"
	reasonCleared           zeroReason = "cleared-element"
)

func (reason zeroReason) description() string {
	switch reason {
	case reasonZeroValue:
		return "value is zero on this path"
	case reasonInputMayBeZero:
		return "input may be zero; no construction establishes a nonzero value"
	case reasonSourcePermitsZero:
		return "the source type permits zero"
	case reasonConversion:
		return "conversion may produce zero without validation"
	case reasonUncheckedError:
		return "result is used before its error is checked"
	case reasonUncheckedLookup:
		return "map lookup may return zero when the key is absent; check ok"
	case reasonUncheckedReceive:
		return "channel receive may return zero when the channel is closed; check ok"
	case reasonUncheckedAssert:
		return "type assertion may return zero when it fails; check ok"
	case reasonZeroStorage:
		return "storage starts at zero; nonzero initialization has not been established"
	case reasonCleared:
		return "clear sets collection elements to zero"
	}
	return ""
}

func (c *zeroChecker) report(use zeroUse, problem *zeroProblem, operation string, value ssa.Value) {
	key := zeroReport{use.pos, problem.name, problem.path}
	if !use.pos.IsValid() || c.reported[key] {
		return
	}
	c.reported[key] = true
	site := c.useSite(use, value, operation, problem.path)
	pos, operation := site.pos, site.operation
	// Presentation is relative to the source use; analysis and deduplication
	// retain the original path through any compiler-generated packing slice.
	if problem.path != site.path {
		local := *problem
		local.path = site.path
		problem = &local
	}
	certainty := diagnostic.Known
	if problem.possible {
		certainty = diagnostic.Possible
	}
	if len(problem.provenance) == 0 {
		problem = problem.source(pos, problem.reason.description())
	} else {
		problem = problem.source(problem.provenance[0].pos, problem.reason.description())
	}
	if problem.value == "" {
		problem = problem.withValue(c.valueName(value, nil))
	}
	problem = problem.step("use", pos, operation)
	finding := diagnostic.Finding{
		Rule: "domainzero.invalid-zero", Certainty: certainty,
		Operation: operation, Value: problem.value, Type: typeIdentity(problem.typ),
		Path: problem.path, Reason: string(problem.reason), Pos: pos,
		Message: problem.message(operation),
	}
	for _, step := range problem.provenance {
		finding.Provenance = append(finding.Provenance, diagnostic.Step{
			Kind: step.kind, Pos: step.pos, Message: step.detail,
		})
	}
	c.pass.Report(finding.Diagnostic())
}

func (problem *zeroProblem) message(operation string) string {
	value, invariant := "a zero "+problem.name, "zero values are invalid"
	if basic, ok := problem.typ.Underlying().(*types.Basic); ok && basic.Info()&types.IsString != 0 {
		value, invariant = "an empty "+problem.name, "empty values are invalid"
	}
	subject, definite, possible := operation, "uses", "may use"
	switch operation {
	case "return":
		definite, possible = "produces", "may produce"
		if problem.path != "" {
			definite, possible = "contains", "may contain"
		}
	case "argument", "variadic-argument", "receiver", "map-key", "map-value", "operand", "append", "copy", "clear":
		definite, possible = "is", "may be"
		switch operation {
		case "variadic-argument":
			subject = "variadic argument"
		case "append":
			subject = "append argument"
		case "copy":
			subject = "copy source"
		case "clear":
			subject = "slice element after clear"
		case "map-key":
			subject = "map key"
		case "map-value":
			subject = "map value"
		}
		if problem.path != "" {
			definite, possible = "contains", "may contain"
		}
	case "send":
		subject, definite, possible = "channel send", "uses", "may use"
	case "assignment", "store-element":
		subject, definite, possible = "assignment", "stores", "may store"
	}
	verb := definite
	if problem.possible {
		verb = possible
	}
	message := subject + " " + verb + " " + value
	if problem.path != "" {
		message += " at path " + problem.path
	}
	return message + "; " + invariant
}

func (problem *zeroProblem) withValue(value string) *zeroProblem {
	if problem == nil || problem.value != "" || value == "" {
		return problem
	}
	copy := *problem
	copy.value = value
	return &copy
}
