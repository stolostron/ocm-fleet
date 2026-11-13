package domainconstruct

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/diagnostic"
)

// Reasons describe evidence, not English fragments used to classify output.
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
	reasonMutation          zeroReason = "possible-mutation"
	reasonBranch            zeroReason = "branch-may-be-zero"
	reasonUninitialized     zeroReason = "uninitialized-element"
	reasonCleared           zeroReason = "cleared-element"
)

type sourceExpression struct {
	node ast.Expr
	text string
}

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
	case reasonMutation:
		return "a call may leave or introduce zero"
	case reasonBranch:
		return "a control-flow alternative may supply zero"
	case reasonUninitialized:
		return "collection element starts at zero and has not been initialized"
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
	pos := c.usePosition(use, value, problem.path)
	certainty := diagnostic.Known
	if problem.possible {
		certainty = diagnostic.Possible
	}
	if len(problem.provenance) == 0 {
		problem = problem.source(pos, problem.reason.description())
	} else {
		problem = problem.source(problem.provenance[0].pos, problem.reason.description())
	}
	problem = problem.withValue(c.valueName(value, nil)).step("use", pos, operation)
	finding := diagnostic.Finding{
		Rule: "domainzero.invalid-zero", Certainty: certainty,
		Operation: operation, Value: problem.value, Type: typeIdentity(problem.typ),
		FieldPath: problem.path, Reason: string(problem.reason), Pos: pos,
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
	case "argument", "receiver", "map-key", "map-value", "operand":
		definite, possible = "is", "may be"
		switch operation {
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
	case "clear":
		definite, possible = "produces", "may produce"
	}
	verb := definite
	if problem.possible {
		verb = possible
	}
	message := subject + " " + verb + " " + value
	if problem.path != "" {
		message += " in field " + problem.path
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

func (c *zeroChecker) valueName(value ssa.Value, path []int) string {
	if value == nil {
		return ""
	}
	if len(path) > 0 {
		if name := c.valueName(value, nil); name != "" {
			return projectName(name, value.Type(), path)
		}
		return ""
	}
	switch value := value.(type) {
	case *ssa.Parameter, *ssa.FreeVar, *ssa.Alloc:
		return projectedName(value, nil)
	case *ssa.Const:
		if value.Value == nil {
			return "nil"
		}
		return value.Value.ExactString()
	case *ssa.Extract:
		return c.valueName(value.Tuple, nil)
	}
	return c.expressionAt(value.Pos())
}

func (c *zeroChecker) expressionAt(pos token.Pos) string {
	expression := c.sourceExpression(pos)
	if expression.node != nil && expression.text == "" {
		expression.text = expressionText(c.pass.Fset, expression.node)
		c.expressions[pos] = expression
	}
	return expression.text
}

func (c *zeroChecker) sourceExpression(pos token.Pos) sourceExpression {
	if !pos.IsValid() {
		return sourceExpression{}
	}
	if expression, ok := c.expressions[pos]; ok {
		return expression
	}
	var expression ast.Expr
	for _, file := range c.pass.Files {
		if pos < file.Pos() || pos > file.End() {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil || pos < node.Pos() || pos >= node.End() {
				return false
			}
			if candidate, ok := node.(ast.Expr); ok {
				expression = candidate
			}
			return true
		})
		break
	}
	source := sourceExpression{node: expression}
	c.expressions[pos] = source
	return source
}
