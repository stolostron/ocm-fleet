package domainconstruct

import (
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

func (c *zeroChecker) callContract(call *ssa.CallCommon) *functionContract {
	if function := call.StaticCallee(); function != nil {
		if object, ok := function.Object().(*types.Func); ok {
			return c.contracts.functions[object.Origin()]
		}
	}
	return nil
}

func contractArgument(call *ssa.CallCommon, index int) ssa.Value {
	if function := call.StaticCallee(); function != nil && function.Signature.Recv() != nil && !call.IsInvoke() {
		index++
	}
	if index < len(call.Args) {
		return call.Args[index]
	}
	return nil
}

func (c *zeroChecker) callTruth(call *ssa.Call, result int, context flowContext, seen map[ssa.Value]bool) truth {
	if function := call.Call.StaticCallee(); function != nil && function.Pkg != nil {
		path, name := function.Pkg.Pkg.Path(), function.Name()
		if path == "errors" && name == "New" || path == "fmt" && name == "Errorf" {
			return notZero
		}
		if path == "errors" && name == "Join" {
			return c.joinedErrorTruth(call.Call.Args[0], instructionPoint(call), context, seen)
		}
		if path == "context" && name == "Cause" && c.contextCanceled(call.Call.Args[0], call, context) {
			return notZero
		}
	}
	if receiver := contextReceiver(&call.Call, "Err"); receiver != nil && c.contextCanceled(receiver, call, context) {
		return notZero
	}
	if fact := c.callContract(&call.Call); fact != nil {
		for _, rule := range fact.Errors {
			if rule.Result != result {
				continue
			}
			if rule.Parameter == -1 {
				return notZero
			}
			if argument := contractArgument(&call.Call, rule.Parameter); argument != nil {
				actual := c.valueTruth(argument, context, seen)
				if actual == notZero || rule.Exact && actual == isZero {
					return actual
				}
			}
		}
	}
	return unknown
}

func contextReceiver(call *ssa.CallCommon, method string) ssa.Value {
	if call.IsInvoke() && call.Method.Pkg() != nil && call.Method.Pkg().Path() == "context" && call.Method.Name() == method {
		return call.Value
	}
	return nil
}

func (c *zeroChecker) contextCanceled(receiver ssa.Value, at *ssa.Call, context flowContext) bool {
	for _, block := range at.Parent().Blocks {
		if !block.Dominates(at.Block()) {
			continue
		}
		for _, instruction := range block.Instrs {
			if instruction == at {
				break
			}
			switch instruction := instruction.(type) {
			case *ssa.UnOp:
				if instruction.Op == token.ARROW {
					if call, ok := instruction.X.(*ssa.Call); ok && contextReceiver(&call.Call, "Done") == receiver {
						return true
					}
				}
			case *ssa.Select:
				for index, state := range instruction.States {
					if call, ok := state.Chan.(*ssa.Call); ok && state.Dir == types.RecvOnly && contextReceiver(&call.Call, "Done") == receiver && selectedState(instruction, index, context) {
						return true
					}
				}
			}
		}
	}
	return false
}

func selectedState(selection *ssa.Select, index int, context flowContext) bool {
	for value, state := range context.facts {
		if comparison, ok := value.(*ssa.BinOp); ok && (comparison.Op == token.EQL || comparison.Op == token.NEQ) {
			if comparison.Op == token.NEQ {
				state = opposite(state)
			}
			if state != notZero {
				continue
			}
			for _, pair := range [][2]ssa.Value{{comparison.X, comparison.Y}, {comparison.Y, comparison.X}} {
				extract, ok := pair[0].(*ssa.Extract)
				literal, constantValue := pair[1].(*ssa.Const)
				if ok && extract.Tuple == selection && extract.Index == 0 && constantValue && literal.Value != nil && literal.Value.Kind() == constant.Int && literal.Int64() == int64(index) {
					return true
				}
			}
		}
	}
	return false
}
