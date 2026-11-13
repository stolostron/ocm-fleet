package zeroflow

func LabelReturn() ID {
	var bad ID
	return bad // want "rule: domainzero[.]invalid-zero"
}

func LabelReturnStruct() Request {
	return Request{} // want "rule: domainzero[.]invalid-zero"
}

func LabelReturnCollection() []ID {
	return make([]ID, 1) // want "rule: domainzero[.]invalid-zero"
}

func LabelMapKey(values map[ID]string) {
	var bad ID
	values[bad] = "ok" // want "rule: domainzero[.]invalid-zero"
}

func LabelMapValue(values map[string]ID) {
	var bad ID
	values["ok"] = bad // want "rule: domainzero[.]invalid-zero"
}

func LabelMapCollectionValue(values map[string][1]ID) {
	values["ok"] = [1]ID{} // want "rule: domainzero[.]invalid-zero"
}

func LabelSend(ch chan<- ID) {
	var bad ID
	ch <- bad // want "rule: domainzero[.]invalid-zero"
}

func LabelSelectSend(ch chan<- ID) {
	var bad ID
	select {
	case ch <- bad: // want "rule: domainzero[.]invalid-zero"
	default:
	}
}

func LabelAssignment(destination *ID) {
	var bad ID
	*destination = bad // want "rule: domainzero[.]invalid-zero"
}

func LabelConversion() string {
	var bad ID
	return string(bad) // want "rule: domainzero[.]invalid-zero"
}

func LabelOperand(valid ID) bool {
	var bad ID
	return bad == valid // want "rule: domainzero[.]invalid-zero"
}

func LabelClearNested(values []Request) {
	clear(values) // want "rule: domainzero[.]invalid-zero"
}
