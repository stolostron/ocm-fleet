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
	errorMerges map[*ssa.Function][]*ssa.Phi
	mutations   map[mutationQuery]mutationKind
	reads       map[ssa.Value]storageRead
	stores      map[ssa.Value][]storageWrite
	zeroStorage map[valueVisit]bool
	zeroTypes   map[zeroTypeVisit]*zeroProblem
	jsonShape   jsonFieldCache
	reported    map[zeroReport]bool
	interesting map[types.Type]bool
	locating    map[ssa.Value]bool
	returns     map[token.Pos]bool
	comparisons map[token.Pos]bool
	expressions map[token.Pos]sourceExpression
	deferred    map[*ssa.Function][]*ssa.Defer
	work        *workBudget
}

type zeroTypeVisit struct {
	typ types.Type
	use valueUse
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
	pos       token.Pos
	typ       types.Type
	path      string
	operation string
	argument  int
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
		work:        new(workBudget),
	}
	for _, finding := range c.contracts.presenceDiagnostics {
		pass.Report(finding)
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
			if problem := c.zeroType(object.Type(), "", wholeValue); problem != nil {
				pass.Report((diagnostic.Finding{
					Rule: "domainzero.invalid-zero-policy", Certainty: diagnostic.Known,
					Operation: "declaration", Value: object.Name(), Type: typeIdentity(p.typ), Path: problem.path,
					Reason: "required-field-invalid-zero", Pos: object.Pos(),
					Message: fmt.Sprintf("%s declares a valid zero but required value at path %s has invalid zero %s", p.name, problem.path, problem.name),
				}).Diagnostic())
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
			c.resetWork()
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
	c.resetWork()
	check := func(value ssa.Value, operation string, mode valueUse, argument int) {
		if mode == outputStorage {
			return
		}
		useContext := context
		useContext.use = mode
		if problem := c.problem(value, nil, value.Type(), point, useContext, make(map[valueVisit]bool)); problem != nil {
			use := zeroUse{pos: instructionPosition(instruction, point), argument: argument}
			if call, ok := instruction.(ssa.CallInstruction); ok {
				use.call = call.Common()
			}
			c.report(use, problem, operation, value)
		}
	}
	switch instruction := instruction.(type) {
	case ssa.CallInstruction:
		c.checkCall(instruction, point, context, false)
	case *ssa.RunDefers:
		c.checkCleanup(point, context)
	case *ssa.Return:
		c.checkResults(instruction.Results, point, context, instructionPosition(instruction, point))
	case *ssa.MapUpdate:
		check(instruction.Key, "map-key", wholeValue, 0)
		check(instruction.Value, "map-value", wholeValue, 0)
	case *ssa.Send:
		check(instruction.X, "send", wholeValue, 0)
	case *ssa.Select:
		for _, state := range instruction.States {
			if state.Dir == types.SendOnly && c.valueTruth(state.Chan, context, make(map[ssa.Value]bool)) != isZero {
				if problem := c.problem(state.Send, nil, state.Send.Type(), point, context, make(map[valueVisit]bool)); problem != nil {
					c.report(zeroUse{pos: state.Pos}, problem, "send", state.Send)
				}
			}
		}
	case *ssa.Lookup:
		check(instruction.Index, "map-key", wholeValue, 0)
	case *ssa.BinOp:
		if c.comparisons[instruction.Pos()] {
			return
		}
		for index, operand := range []ssa.Value{instruction.X, instruction.Y} {
			if c.ownRepresentation(operand.Type(), point) {
				// A constructor may inspect its representation before it is valid.
				continue
			}
			// Comparing pointers observes identity or absence, not domain state.
			if _, pointer := operand.Type().Underlying().(*types.Pointer); !pointer {
				check(operand, "operand", wholeValue, index)
			}
		}
	case *ssa.Convert:
		if !c.representationConversion(instruction.X.Type(), instruction.Type(), point) {
			check(instruction.X, "conversion", wholeValue, 0)
		}
	case *ssa.ChangeType:
		if !types.Identical(instruction.Type(), instruction.X.Type()) && !c.representationConversion(instruction.X.Type(), instruction.Type(), point) {
			check(instruction.X, "conversion", wholeValue, 0)
		}
	case *ssa.Store:
		if c.checkYieldReturn(instruction, point, context) {
			return
		}
		if address, ok := c.collectionAddress(instruction.Addr); ok {
			if !c.freshStorage(address.root) {
				check(instruction.Val, "assignment", wholeValue, 0)
			}
		}
	}
}

func (c *zeroChecker) checkCall(instruction ssa.CallInstruction, point flowPoint, context flowContext, executingDefer bool) {
	call := instruction.Common()
	if typeOnlyCall(call) {
		return
	}
	_, deferred := instruction.(*ssa.Defer)
	if (!deferred || executingDefer) && c.checkCollectionBuiltin(instruction, point, context) {
		return
	}
	if deferred && !executingDefer {
		if _, builtin := call.Value.(*ssa.Builtin); builtin {
			return // Collection builtins observe their captured backing storage at execution.
		}
	}
	check := func(value ssa.Value, operation string, index int) {
		use := c.argumentUse(call, index)
		if use == outputStorage || !c.interestingType(value.Type()) {
			return
		}
		if deferred && deferredReference(value) != executingDefer {
			return
		}
		useContext := context
		useContext.use = use
		if problem := c.problem(value, nil, value.Type(), point, useContext, make(map[valueVisit]bool)); problem != nil {
			ref := zeroUse{pos: instructionPosition(instruction, point), call: call, argument: index}
			c.report(ref, problem, operation, value)
		}
	}
	check(call.Value, "receiver", -1)
	for index, argument := range call.Args {
		check(argument, "argument", index)
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

func (c *zeroChecker) zeroType(t types.Type, path string, use valueUse) *zeroProblem {
	visit := zeroTypeVisit{t, use}
	problem, cached := c.zeroTypes[visit]
	if !cached {
		problem = c.zeroTypeShape(t, use)
		if c.zeroTypes == nil {
			c.zeroTypes = make(map[zeroTypeVisit]*zeroProblem)
		}
		c.zeroTypes[visit] = problem
	}
	if problem == nil {
		return nil
	}
	// Callers attach their own location and provenance to the cached shape.
	result := *problem
	result.path = fieldPath(path, result.path)
	return &result
}

func (c *zeroChecker) zeroTypeShape(t types.Type, use valueUse) *zeroProblem {
	if optionalPayload(t) != nil {
		return nil
	}
	if _, pointer := types.Unalias(t).(*types.Pointer); pointer {
		return nil
	}
	if p := c.policies.lookup(t); p != nil && !p.fact.ZeroValid {
		return &zeroProblem{name: p.name, typ: p.typ, reason: reasonZeroValue}
	}
	use = c.useForType(t, use)
	switch t := t.Underlying().(type) {
	case *types.Struct:
		return c.zeroFields(t, "", use)
	case *types.Array:
		if t.Len() != 0 {
			return c.zeroType(t.Elem(), "[0]", use)
		}
	}
	return nil
}

func (c *zeroChecker) zeroFields(structure *types.Struct, path string, use valueUse) *zeroProblem {
	for selected := range c.jsonShape.fields(structure, use.json()) {
		if !selected.indirect {
			if problem := c.zeroType(selected.field.Type(), fieldPath(path, selected.path), use); problem != nil {
				return problem
			}
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

func isBuiltinError(t types.Type) bool {
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

func (c *zeroChecker) errorStatus(t types.Type) bool {
	if c.policies.lookup(t) != nil {
		return false
	}
	switch t.Underlying().(type) {
	case *types.Interface, *types.Pointer, *types.Map, *types.Slice, *types.Chan, *types.Signature:
		return isErrorType(t)
	}
	return false
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
