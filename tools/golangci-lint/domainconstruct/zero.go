package domainconstruct

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"

	"github.com/stolostron/ocm-fleet/tools/golangci-lint/diagnostic"
)

// ZeroAnalyzer checks whether invalid zero values reach domain operations.
var ZeroAnalyzer = &analysis.Analyzer{
	Name:     "domainzero",
	Doc:      "report invalid zero domain values at their uses",
	Run:      runZero,
	Requires: []*analysis.Analyzer{Analyzer, contractsAnalyzer, buildssa.Analyzer},
}

type zeroChecker struct {
	pass        *analysis.Pass
	policies    policySet
	contracts   *contractSet
	contexts    map[*ssa.BasicBlock]flowContext
	equalities  map[*ssa.Function][]*ssa.BinOp
	mutations   map[mutationQuery]mutationKind
	stores      map[ssa.Value][]storageWrite
	zeroStorage map[valueVisit]bool
	reported    map[zeroReport]bool
	interesting map[types.Type]bool
	locating    map[ssa.Value]bool
	returns     map[token.Pos]bool
	comparisons map[token.Pos]bool
	expressions map[token.Pos]sourceExpression
}

type zeroProblem struct {
	name       string
	typ        types.Type
	value      string
	path       string
	reason     zeroReason // Zero source or missing validity contract, independent of certainty.
	possible   bool
	provenance []zeroStep
}

type zeroStep struct {
	kind   string
	pos    token.Pos
	detail string
}

// Alternatives share one certainty rule across SSA values, storage, collections,
// and generic summaries: zero on only some feasible paths is a possible zero.
type zeroAlternatives struct {
	first    *zeroProblem
	paths    int
	zeros    int
	possible bool
}

// add reports whether further paths cannot change the selected problem or its
// certainty. Once a zero is possible, later paths cannot make it definite or
// replace the first problem's provenance.
func (alternatives *zeroAlternatives) add(problem *zeroProblem) bool {
	alternatives.paths++
	if problem == nil {
		return alternatives.first != nil
	}
	alternatives.zeros++
	if alternatives.first == nil {
		alternatives.first = problem
	}
	alternatives.possible = alternatives.possible || problem.possible ||
		problem.name != alternatives.first.name || problem.path != alternatives.first.path
	return alternatives.possible || alternatives.zeros != alternatives.paths
}

func (alternatives zeroAlternatives) problem() *zeroProblem {
	if alternatives.possible || alternatives.zeros != alternatives.paths {
		return alternatives.first.uncertain()
	}
	return alternatives.first
}

type zeroReport struct {
	pos  token.Pos
	name string
	path string
}

type flowPoint struct {
	block *ssa.BasicBlock
	index int
}

func runZero(pass *analysis.Pass) (any, error) {
	c := &zeroChecker{
		pass:        pass,
		policies:    pass.ResultOf[Analyzer].(policySet),
		contracts:   pass.ResultOf[contractsAnalyzer].(*contractSet),
		contexts:    make(map[*ssa.BasicBlock]flowContext),
		reported:    make(map[zeroReport]bool),
		interesting: make(map[types.Type]bool),
		locating:    make(map[ssa.Value]bool),
		returns:     returnPositions(pass.Files),
		comparisons: make(map[token.Pos]bool),
		expressions: make(map[token.Pos]sourceExpression),
	}
	c.checkZeroComparisons()
	for object, positions := range c.contracts.writes {
		if object.Pkg() != pass.Pkg && c.contracts.sentinels[object] {
			for _, pos := range positions {
				pass.Report((diagnostic.Finding{
					Rule: "domainzero.error-sentinel-write", Certainty: diagnostic.Known,
					Operation: "mutation", Value: object.Name(), Type: typeIdentity(object.Type()),
					Reason: "non-nil-sentinel-required", Pos: pos,
					Message: fmt.Sprintf("do not reassign or expose the address of non-nil error sentinel %s", object.Name()),
				}).Diagnostic())
			}
		}
	}
	for object, p := range c.policies {
		if object.Pkg() == pass.Pkg && p != nil && p.fact.ZeroValid {
			if structure, ok := object.Type().Underlying().(*types.Struct); ok {
				if problem := c.zeroFields(structure, ""); problem != nil {
					pass.Report((diagnostic.Finding{
						Rule: "domainzero.invalid-zero-policy", Certainty: diagnostic.Known,
						Operation: "declaration", Value: object.Name(), Type: typeIdentity(p.typ), Path: problem.path,
						Reason: "required-field-invalid-zero", Pos: object.Pos(),
						Message: fmt.Sprintf("%s declares a valid zero but required value at path %s has invalid zero %s", p.name, problem.path, problem.name),
					}).Diagnostic())
				}
			}
		}
	}
	flowNeeded := false
	for _, p := range c.policies {
		if p != nil && !p.fact.ZeroValid {
			flowNeeded = true
			break
		}
	}
	if !flowNeeded {
		return nil, nil // Interfaces alone cannot introduce an enrolled domain type.
	}
	input := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	c.indexStores(input.SrcFuncs)
	for _, function := range input.SrcFuncs {
		for _, block := range function.Blocks {
			context := c.contextAt(block)
			if !context.possible {
				continue
			}
			for index, instruction := range block.Instrs {
				c.checkInstruction(instruction, flowPoint{block, index}, context)
				if call, ok := instruction.(*ssa.Call); ok && testingExit(&call.Call) {
					break
				}
			}
		}
	}
	return nil, nil
}

func (c *zeroChecker) checkInstruction(instruction ssa.Instruction, point flowPoint, context flowContext) {
	check := func(value ssa.Value, operation string) {
		if problem := c.problem(value, nil, value.Type(), point, context, make(map[valueVisit]bool)); problem != nil {
			use := zeroUse{pos: instructionPosition(instruction, point)}
			if call, ok := instruction.(ssa.CallInstruction); ok {
				use.call = call.Common()
			}
			c.report(use, problem, operation, value)
		}
	}
	switch instruction := instruction.(type) {
	case ssa.CallInstruction:
		if typeOnlyCall(instruction.Common()) {
			return
		}
		if c.checkCollectionBuiltin(instruction, point, context) {
			return
		}
		if !instruction.Common().IsInvoke() || !c.decodingArgument(instruction.Common(), -1) {
			check(instruction.Common().Value, "receiver")
		}
		for index, argument := range instruction.Common().Args {
			if !c.decodingArgument(instruction.Common(), index) {
				check(argument, "argument")
			}
		}
	case *ssa.Return:
		c.checkResults(instruction.Results, point, context, instructionPosition(instruction, point))
	case *ssa.MapUpdate:
		check(instruction.Key, "map-key")
		check(instruction.Value, "map-value")
	case *ssa.Send:
		check(instruction.X, "send")
	case *ssa.Select:
		for _, state := range instruction.States {
			if state.Dir == types.SendOnly {
				if problem := c.problem(state.Send, nil, state.Send.Type(), point, context, make(map[valueVisit]bool)); problem != nil {
					c.report(zeroUse{pos: state.Pos}, problem, "send", state.Send)
				}
			}
		}
	case *ssa.Lookup:
		check(instruction.Index, "map-key")
	case *ssa.BinOp:
		if c.comparisons[instruction.Pos()] {
			return
		}
		for _, operand := range []ssa.Value{instruction.X, instruction.Y} {
			if c.ownRepresentation(operand.Type(), point) {
				// A constructor may inspect its representation before it is valid.
				continue
			}
			// Comparing pointers observes identity or absence, not domain state.
			if _, pointer := operand.Type().Underlying().(*types.Pointer); !pointer {
				check(operand, "operand")
			}
		}
	case *ssa.Convert:
		if !c.representationConversion(instruction.X.Type(), instruction.Type(), point) {
			check(instruction.X, "conversion")
		}
	case *ssa.ChangeType:
		if !types.Identical(instruction.Type(), instruction.X.Type()) && !c.representationConversion(instruction.X.Type(), instruction.Type(), point) {
			check(instruction.X, "conversion")
		}
	case *ssa.Store:
		if c.checkYieldReturn(instruction, point, context) {
			return
		}
		if address, ok := c.collectionAddress(instruction.Addr); ok {
			if !c.freshStorage(address.root) {
				check(instruction.Val, "assignment")
			}
		}
	}
}

func (c *zeroChecker) ownRepresentation(t types.Type, point flowPoint) bool {
	p := c.policies.lookup(t)
	function, declared := point.block.Parent().Object().(*types.Func)
	return p != nil && declared && p.functions[function.Origin()]
}

func (c *zeroChecker) representationConversion(source, target types.Type, point flowPoint) bool {
	_, scalar := target.Underlying().(*types.Basic)
	return scalar && c.policies.lookup(target) == nil && types.Identical(source.Underlying(), target.Underlying()) && c.ownRepresentation(source, point)
}

func typeOnlyCall(call *ssa.CallCommon) bool {
	function := call.StaticCallee()
	return function != nil && function.Pkg != nil && function.Pkg.Pkg.Path() == "reflect" && function.Name() == "TypeOf"
}

func instructionPosition(instruction ssa.Instruction, point flowPoint) token.Pos {
	if pos := instruction.Pos(); pos.IsValid() {
		return pos
	}
	// Implicit operations such as boxing an argument into an interface have
	// no position. Attribute the use to the next operation with source syntax.
	for _, next := range point.block.Instrs[point.index+1:] {
		if pos := next.Pos(); pos.IsValid() {
			return pos
		}
	}
	return instruction.Parent().Pos()
}

func (problem *zeroProblem) source(pos token.Pos, detail string) *zeroProblem {
	if problem == nil || !pos.IsValid() {
		return problem
	}
	for _, step := range problem.provenance {
		if step.kind == "source" {
			return problem
		}
	}
	copy := *problem
	copy.provenance = append([]zeroStep{{"source", pos, detail}}, problem.provenance...)
	return &copy
}

func (problem *zeroProblem) step(kind string, pos token.Pos, detail string) *zeroProblem {
	if problem == nil || !pos.IsValid() {
		return problem
	}
	step := zeroStep{kind, pos, detail}
	for _, existing := range problem.provenance {
		if existing == step {
			return problem
		}
	}
	copy := *problem
	copy.provenance = append(append([]zeroStep(nil), problem.provenance...), step)
	// Keep source context and the most recent transformations without allowing
	// long control-flow paths to produce unbounded diagnostic payloads.
	if len(copy.provenance) > 24 {
		copy.provenance = append(copy.provenance[:12:12], copy.provenance[len(copy.provenance)-12:]...)
	}
	return &copy
}

// uncertain changes confidence at the use without replacing the zero source.
// Branches and unknown effects explain uncertainty through provenance.
func (problem *zeroProblem) uncertain() *zeroProblem {
	if problem == nil {
		return nil
	}
	copy := *problem
	copy.possible = true
	return &copy
}

// afterUnknownMutation preserves an existing zero concern. The call may
// initialize the value, but unknown effects cannot establish that guarantee.
func (problem *zeroProblem) afterUnknownMutation(pos token.Pos, operation, target string) *zeroProblem {
	if problem == nil {
		return nil
	}
	return problem.uncertain().step("mutation", pos,
		operation+" may modify "+target+"; nonzero initialization afterward is unproven")
}

func (c *zeroChecker) zeroType(t types.Type, path string) *zeroProblem {
	if _, pointer := types.Unalias(t).(*types.Pointer); pointer {
		return nil
	}
	if p := c.policies.lookup(t); p != nil && !p.fact.ZeroValid {
		return &zeroProblem{name: p.name, typ: p.typ, path: path, reason: reasonZeroValue}
	}
	switch t := t.Underlying().(type) {
	case *types.Struct:
		return c.zeroFields(t, path)
	case *types.Array:
		if t.Len() != 0 {
			return c.zeroType(t.Elem(), path+"[0]")
		}
	}
	return nil
}

func (c *zeroChecker) zeroFields(structure *types.Struct, path string) *zeroProblem {
	for i := range structure.NumFields() {
		field := structure.Field(i)
		if problem := c.zeroType(field.Type(), fieldPath(path, field.Name())); problem != nil {
			return problem
		}
	}
	return nil
}

func fieldPath(parent, field string) string {
	if field == "" {
		return parent
	}
	if strings.HasPrefix(field, "[") {
		return parent + field
	}
	return strings.TrimPrefix(parent+"."+field, ".")
}

func isErrorType(t types.Type) bool {
	return types.Implements(t, types.Universe.Lookup("error").Type().Underlying().(*types.Interface))
}

func zeroConstant(value *ssa.Const) bool {
	if value.Value == nil {
		return true
	}
	return constantZero(value.Value)
}

func constantZero(value constant.Value) bool {
	if value == nil {
		return false
	}
	switch value.Kind() {
	case constant.String:
		return constant.StringVal(value) == ""
	case constant.Bool:
		return !constant.BoolVal(value)
	case constant.Int, constant.Float:
		return constant.Sign(value) == 0
	case constant.Complex:
		return constant.Sign(constant.Real(value)) == 0 && constant.Sign(constant.Imag(value)) == 0
	}
	return false
}
