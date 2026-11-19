package zeroflow

type errorForwarder struct{}

func (*errorForwarder) pass(err error) error { return err }

func MethodExpressionError() (ID, error) {
	return "", (*errorForwarder).pass(new(errorForwarder), nil) // want "return produces an empty zeroflow.ID"
}

func MethodValueError() (ID, error) {
	pass := new(errorForwarder).pass
	return "", pass(nil) // want "return produces an empty zeroflow.ID"
}

func FirstStatusOnly(lookup func() (ID, error, error), use func(ID)) {
	id, first, _ := lookup()
	if first == nil {
		use(id) // want "argument may be an empty zeroflow.ID"
	}
}

func BothStatuses(lookup func() (ID, error, error), use func(ID)) {
	id, first, second := lookup()
	if first == nil && second == nil {
		use(id)
	}
}

func RepeatedOperand(use func(ID, ID)) {
	var id ID
	use(id, id) // want "argument is an empty zeroflow.ID" "argument is an empty zeroflow.ID"
}

func InvalidKeyAndValue(out map[ID]ID) {
	var id ID
	out[id] = id // want "map key is an empty zeroflow.ID" "map value is an empty zeroflow.ID"
}

func tupleStatus() (error, bool)             { return nil, false }
func relayStatus(status error, _ bool) error { return status }

func forwardedStatus[T ~string]() (T, error) {
	return "", relayStatus(tupleStatus())
}

func UseForwardedStatus(use func(ID)) {
	id, err := forwardedStatus[ID]()
	if err == nil {
		use(id) // want "argument is an empty zeroflow.ID"
	}
}

type concreteStatus struct{}

func (*concreteStatus) Error() string { return "status" }

func concreteStatusTuple() (*concreteStatus, bool) { return nil, false }
func relayGenericStatus[E error](status E, _ bool) E {
	return status
}

func forwardedConcreteStatus[T ~string]() (T, *concreteStatus) {
	return "", relayGenericStatus(concreteStatusTuple())
}

func UseForwardedConcreteStatus(use func(ID)) {
	id, err := forwardedConcreteStatus[ID]()
	if err == nil {
		use(id) // want "argument is an empty zeroflow.ID"
	}
}

func forwardedBoxedStatus[T ~string]() (T, error) {
	return "", relayGenericStatus[error](concreteStatusTuple())
}

func UseForwardedBoxedStatus(use func(ID)) {
	id, err := forwardedBoxedStatus[ID]()
	if err == nil {
		use(id)
	}
}

func fromRune[T ~string](value rune) T   { return T(value) }
func fromText[T ~string](value string) T { return T(value) }

func UseConvertedSource(use func(ID)) {
	use(fromRune[ID](0))
	use(fromText[ID]("")) // want "argument is an empty zeroflow.ID"
}

func EmptyBoxedString() ID {
	var raw any = ""
	return ID(raw.(string)) // want "return produces an empty zeroflow.ID"
}

func NonemptyBoxedString() ID {
	var raw any = "valid"
	return ID(raw.(string))
}

func CheckedBoxedString() (ID, bool) {
	var raw any = ""
	value, ok := raw.(string)
	if ok {
		return ID(value), true // want "return produces an empty zeroflow.ID"
	}
	return ID("valid"), true
}
