package domainconstruct

import (
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/ssa"
)

func (c *zeroChecker) callContract(call *ssa.CallCommon) *functionContract {
	if function := callObject(call); function != nil {
		return c.contracts.functions[function.Origin()]
	}
	return nil
}

func callObject(call *ssa.CallCommon) *types.Func {
	if call.IsInvoke() {
		return call.Method
	}
	if function := call.StaticCallee(); function != nil {
		object, _ := function.Object().(*types.Func)
		return object
	}
	return nil
}

func contractArgument(call *ssa.CallCommon, index int) ssa.Value {
	if index == -1 {
		if call.IsInvoke() {
			return call.Value
		}
		if closure, ok := call.Value.(*ssa.MakeClosure); ok && len(closure.Bindings) == 1 {
			if method, ok := closure.Fn.(*ssa.Function); ok && strings.HasPrefix(method.Synthetic, "bound method wrapper") {
				return closure.Bindings[0]
			}
		}
	}
	if index := callArgumentIndex(call, index); index >= 0 && index < len(call.Args) {
		return call.Args[index]
	}
	return nil
}

// Declared parameter indices exclude the receiver. A method-expression thunk
// takes it as an explicit argument, while an invoke or bound closure keeps it
// separate from Args.
func callArgumentIndex(call *ssa.CallCommon, index int) int {
	if call.IsInvoke() {
		return index
	}
	signature := call.Signature()
	if signature.Recv() != nil {
		return index + 1
	}
	if function := callObject(call); function != nil {
		declared := function.Type().(*types.Signature)
		if declared.Recv() != nil && signature.Params().Len() == declared.Params().Len()+1 {
			return index + 1
		}
	}
	return index
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
			actual := unknown
			if rule.Parameter >= 0 {
				if argument := contractArgument(&call.Call, rule.Parameter); argument != nil {
					actual = c.valueTruth(argument, context, seen)
				}
			}
			if result := rule.resultTruth(actual, rule.typeArgument(&call.Call)); result != unknown {
				return result
			}
		}
	}
	return unknown
}

func (rule errorRule) typeArgument(call *ssa.CallCommon) types.Type {
	if index := rule.BoxedZeroType; index != 0 {
		if callee := call.StaticCallee(); callee != nil && index <= len(callee.TypeArgs()) {
			return callee.TypeArgs()[index-1]
		}
	}
	return nil
}

func contextReceiver(call *ssa.CallCommon, method string) ssa.Value {
	if object := callObject(call); object != nil && object.Pkg() != nil && object.Pkg().Path() == "context" && object.Name() == method {
		if receiver := contractArgument(call, -1); receiver != nil && (call.IsInvoke() || exactMethodReceiver(call, receiver)) {
			return receiver
		}
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
