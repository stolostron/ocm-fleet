package zeroflow

type nameExecution[T any] struct{ val T }
type nameView struct{ Deployment Request }
type nameOptional struct{ Entry *Request }

// An anonymous generic wrapper must not acquire the compiler's allocation name.
func NameGenericLiteral() any {
	return &nameExecution[nameView]{val: nameView{}} // want "rule: domainzero[.]invalid-zero"
}

func NamePointerLiteral() *Request {
	return &Request{} // want "rule: domainzero[.]invalid-zero"
}

func NameExplicitZeroField() *Request {
	return &Request{ID: ""} // want "rule: domainzero[.]invalid-zero"
}

func NameArrayLiteral() *[1]Request {
	return &[1]Request{} // want "rule: domainzero[.]invalid-zero"
}

func NameSliceLiteral() []Request {
	return []Request{{}} // want "rule: domainzero[.]invalid-zero"
}

func NameNewStorage() *Request {
	return new(Request) // want "rule: domainzero[.]invalid-zero"
}

func NameNamedStorage() *Request {
	var record Request
	return &record // want "rule: domainzero[.]invalid-zero"
}

// A real variable may have the same name as an internal allocation label.
func NameCompilerLikeVariable() *Request {
	var complit Request
	return &complit // want "rule: domainzero[.]invalid-zero"
}

// SSA represents these aggregate zeros with a nil payload, not a source nil.
func NameZeroStruct() Request {
	return Request{} // want "rule: domainzero[.]invalid-zero"
}

func NameZeroArray() [1]Request {
	return [1]Request{} // want "rule: domainzero[.]invalid-zero"
}

func NameZeroRef() Ref {
	return Ref{} // want "rule: domainzero[.]invalid-zero"
}

func NameNilOption() *Request {
	return nil
}

func NameNonNilOption() *nameOptional {
	return &nameOptional{Entry: &Request{}} // want "rule: domainzero[.]invalid-zero"
}

func NameNamedProjection(input struct{ Name string }) ID {
	return ID(input.Name) // want "rule: domainzero[.]invalid-zero"
}

func NameIndirectMutation(factory func() func(*string)) ID {
	var raw string
	factory()(&raw)
	return ID(raw) // want "rule: domainzero[.]invalid-zero"
}

func NameTupleMutation(factory func() (func(*string), int)) ID {
	var raw string
	mutate, _ := factory()
	mutate(&raw)
	return ID(raw) // want "rule: domainzero[.]invalid-zero"
}

func NameAnonymousMutation(mutate func(*string)) ID {
	var raw string
	func() { mutate(&raw) }()
	return ID(raw) // want "rule: domainzero[.]invalid-zero"
}

func NameNamedClosureMutation(mutate func(*string)) ID {
	var raw string
	update := func() { mutate(&raw) }
	update()
	return ID(raw) // want "rule: domainzero[.]invalid-zero"
}

func NameVariadicLiteral() {
	acceptNames(Request{}) // want "rule: domainzero[.]invalid-zero"
}

func acceptNames(...any) {}
