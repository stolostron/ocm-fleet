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
