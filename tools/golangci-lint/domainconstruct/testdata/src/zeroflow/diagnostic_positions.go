package zeroflow

import "fmt"

func acceptPositionArgument(string, ID) {}
func acceptPositionPair(ID, error)     {}
func acceptPositionRepeated(ID, ID)    {}
func acceptPositionVariadic(string, ...ID) {}
func acceptPositionGeneric[T any](string, T) {}

func (r Ref) acceptPositionArgument(ID) {}
func (r Ref) acceptPositionVariadic(string, ...ID) {}

type positionReader interface{ Key() string }
type positionConsumer interface{ Accept(ID) }

func PositionArgument() {
	var bad ID
	acceptPositionArgument("ok", /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionPointerArgument() {
	var bad Request
	AcceptAny( /* position */ &bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionMultilineArgument() {
	var bad ID
	acceptPositionArgument(
		"ok",
		/* position */ bad, // want "rule: domainzero[.]invalid-zero"
	)
}

func PositionRepeatedArgument() {
	var bad ID
	acceptPositionRepeated( /* position */ bad, bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionMethodReceiver() {
	var bad Ref
	_ = /* position */ bad.Key() // want "rule: domainzero[.]invalid-zero"
}

func PositionPointerReceiver() {
	var bad Ref
	_ = /* position */ (&bad).Key() // want "rule: domainzero[.]invalid-zero"
}

func PositionMethodArgument(valid Ref) {
	var bad ID
	valid.acceptPositionArgument( /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionMethodExpression(valid Ref) {
	var bad ID
	Ref.acceptPositionArgument(valid, /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionMethodExpressionReceiver() {
	var bad Ref
	_ = Ref.Key( /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionInterfaceReceiver() {
	var zero Ref
	var bad positionReader = zero
	_ = /* position */ bad.Key() // want "rule: domainzero[.]invalid-zero"
}

func PositionInterfaceArgument(valid positionConsumer) {
	var bad ID
	valid.Accept( /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionBoundMethodArgument(valid Ref) {
	call := valid.acceptPositionArgument
	var bad ID
	call( /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionBoundMethodReceiver() {
	var zero Ref
	bad := zero.Key
	_ = /* position */ bad() // want "rule: domainzero[.]invalid-zero"
}

func PositionVariadicArgument(valid ID) {
	var bad ID
	acceptPositionVariadic("ok", valid, /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionEllipsisArgument(valid ID) {
	bad := []ID{valid, ""}
	acceptPositionVariadic("ok", /* position */ bad...) // want "rule: domainzero[.]invalid-zero"
}

func PositionVariadicMethodArgument(receiver Ref, valid ID) {
	var bad ID
	receiver.acceptPositionVariadic("ok", valid, /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionVariadicMethodExpression(receiver Ref, valid ID) {
	var bad ID
	Ref.acceptPositionVariadic(receiver, "ok", valid, /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionAppendArgument(destination []ID, valid ID) {
	var bad ID
	_ = append(destination, valid, /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionCopyArgument(destination []ID, valid ID) {
	bad := []ID{valid, ""}
	copy(destination, /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionClearArgument(bad []ID) {
	clear( /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionPackageArgument() {
	var bad ID
	_ = fmt.Sprint("ok", /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionGenericArgument() {
	var bad ID
	acceptPositionGeneric("ok", /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionParenthesizedFunction() {
	var bad ID
	(Accept)( /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionTupleArgument(bad func() (ID, error)) {
	acceptPositionPair( /* position */ bad()) // want "rule: domainzero[.]invalid-zero"
}

func PositionDeferArgument() {
	var bad ID
	defer acceptPositionArgument("ok", /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}

func PositionGoArgument() {
	var bad ID
	go acceptPositionArgument("ok", /* position */ bad) // want "rule: domainzero[.]invalid-zero"
}
