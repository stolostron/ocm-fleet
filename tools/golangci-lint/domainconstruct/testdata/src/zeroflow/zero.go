// Package zeroflow deliberately exercises invalid zero construction and use.
package zeroflow

import (
	"errors"
	"fmt"
)

// ID has an invalid zero.
//
//domain:constructors NewID
type ID string

func NewID(raw string) (ID, error) {
	if raw == "" {
		return "", errors.New("empty identifier")
	}
	return ID(raw), nil
}

// Ref is a composite value with an invalid zero.
//
//domain:constructors NewRef
type Ref struct{ key string }

func NewRef(raw string) (Ref, error) {
	if raw == "" {
		return Ref{}, fmt.Errorf("empty reference")
	}
	return Ref{key: raw}, nil
}

func (r Ref) Key() string { return r.key }

// Total has a meaningful zero.
//
//domain:constructors NewTotal
//domain:zero-valid
type Total int

func NewTotal(raw int) Total { return Total(raw) }

// UnclassifiedEmpty has not opted into a valid zero.
//
//domain:constructors NewUnclassifiedEmpty
type UnclassifiedEmpty struct{}

func NewUnclassifiedEmpty() UnclassifiedEmpty {
	return UnclassifiedEmpty{} // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]UnclassifiedEmpty\""
}

type Request struct {
	ID       ID
	Optional *ID
}

type OptionalRequest struct{ ID *ID }

// Envelope's claimed valid zero contradicts its required field's contract.
//
//domain:constructors NewEnvelope
//domain:zero-valid
type Envelope struct { // want "valid zero.*required field ID.*zeroflow.ID"
	ID ID
}

func NewEnvelope(id ID) Envelope { return Envelope{ID: id} }

func Accept(ID)                      {}
func AcceptRef(Ref)                  {}
func AcceptPointer(*ID)              {}
func AcceptRequest(Request)          {}
func AcceptOptional(OptionalRequest) {}
func AcceptTotal(Total)              {}
func AcceptAny(any)                  {}

// OptionalEnvelope has a valid zero, including nil optional data and containers.
//
//domain:constructors NewOptionalEnvelope
//domain:zero-valid
type OptionalEnvelope struct {
	ID     *ID
	Values []ID
}

func NewOptionalEnvelope(id *ID) OptionalEnvelope { return OptionalEnvelope{ID: id} }

func AcceptEnvelope(OptionalEnvelope) {}

func InterfaceArgument() {
	var id ID
	AcceptAny(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func ZeroStorage() {
	var id ID
	Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func InitializedStorage(raw string) error {
	var id ID
	var err error
	id, err = NewID(raw)
	if err != nil {
		return err
	}
	Accept(id)
	return nil
}

func ConditionalInitialization(condition bool, valid ID) {
	var id ID
	if condition {
		id = valid
	}
	Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func BothBranchesInitialize(condition bool, first, second ID) {
	var id ID
	if condition {
		id = first
	} else {
		id = second
	}
	Accept(id)
}

func EarlyReturnInitializes(condition bool, valid ID) {
	var id ID
	if condition {
		return
	}
	id = valid
	Accept(id)
}

func ZeroReturn() ID {
	var id ID
	return id // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func NamedZeroReturn() (id ID) {
	return // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func ZeroSuccess() (ID, error) {
	return "", nil // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func UnknownError(err error) (ID, error) {
	return "", err // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func KnownError(err error) (ID, error) {
	if err != nil {
		return "", err
	}
	return NewID("valid")
}

func NewlyCreatedError() (Ref, error) { return Ref{}, errors.New("failure") }

func WrappedError(err error) (Ref, error) { return Ref{}, fmt.Errorf("failure: %w", err) }

func ForwardResult(raw string) (ID, error) {
	id, err := NewID(raw)
	return id, err
}

func DeferredError() (Ref, error) {
	defer func() {}()
	return Ref{}, fmt.Errorf("failure")
}

func DeferredForward(raw string) (ID, error) {
	defer func() {}()
	return NewID(raw)
}

func DeferredBranch(raw string) (ID, error) {
	defer func() {}()
	id, err := NewID(raw)
	if err != nil {
		return "", err
	}
	return id, nil
}

func DeferredZeroSuccess() (ID, error) {
	defer func() {}()
	var id ID
	return id, nil // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func IgnoreConstructorError(raw string) {
	id, _ := NewID(raw)
	Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func UseBeforeErrorCheck(raw string) error {
	id, err := NewID(raw)
	Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	return err
}

func SuccessBranch(raw string) {
	id, err := NewID(raw)
	if err == nil {
		Accept(id)
	}
}

func FailureBranch(raw string) {
	id, err := NewID(raw)
	if err != nil {
		Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	}
}

func ReassignedError(raw string) {
	id, err := NewID(raw)
	err = nil
	if err == nil {
		Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	}
}

func ValidZeros() {
	var total Total
	AcceptTotal(total)
	AcceptTotal(0)
	AcceptPointer(nil)
	AcceptOptional(OptionalRequest{})
	AcceptEnvelope(OptionalEnvelope{})
}

func CompositeStorage() {
	var ref Ref
	AcceptRef(ref) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
	_ = ref.Key()  // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func OmittedRequiredField() {
	AcceptRequest(Request{}) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\"; field-path: \"ID\""
}

func InitializedField(valid ID) {
	var request Request
	request.ID = valid
	AcceptRequest(request)
	copy := request
	AcceptRequest(copy)
}

func ConditionalField(condition bool, valid ID) {
	var request Request
	if condition {
		request.ID = valid
	}
	AcceptRequest(request) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\"; field-path: \"ID\""
}

func UninitializedPointer() {
	id := new(ID)
	AcceptPointer(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func InitializedPointer(valid ID) {
	id := new(ID)
	*id = valid
	AcceptPointer(id)
}

func OmittedOptionalField() {
	AcceptOptional(OptionalRequest{ID: new(ID)}) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\"; field-path: \"ID\""
}

type Recursive struct {
	Next *Recursive
	ID   ID
}

func AcceptRecursive(Recursive) {}

func InvalidRecursiveChild(valid ID) {
	child := &Recursive{}
	parent := Recursive{Next: child, ID: valid}
	AcceptRecursive(parent) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\"; field-path: \"Next[.]ID\""
}

func ValidRecursiveChildren(valid ID) {
	child := &Recursive{ID: valid}
	parent := Recursive{Next: child, ID: valid}
	AcceptRecursive(parent)
}

func MapLookup(values map[string]ID, key string) {
	Accept(values[key]) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	id, ok := values[key]
	if !ok {
		return
	}
	Accept(id)
}

func FailedLookup(values map[string]ID, key string) {
	id, ok := values[key]
	if !ok {
		Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	}
}

func MapOfValidZeros(values map[string]Total) { AcceptTotal(values["missing"]) }

func TypeAssertion(value any) {
	id, ok := value.(ID)
	if ok {
		Accept(id)
	}
}

func UncheckedAssertion(value any) {
	id, _ := value.(ID)
	Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func ChannelReceive(values <-chan ID) {
	Accept(<-values) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	id, ok := <-values
	if ok {
		Accept(id)
	}
}

func InvalidContainerWrites(values map[string]ID, ch chan<- ID) {
	var id ID
	values["key"] = id // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	ch <- id           // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func RawConversion() string {
	var id ID
	return string(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func LoopMayNotRun(raw []string) (ID, error) {
	var id ID
	var err error
	for _, value := range raw {
		id, err = NewID(value)
		if err != nil {
			return "", err
		}
	}
	return id, nil // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func CorrelatedReturn(condition bool, raw string) (ID, error) {
	var id ID
	var err error
	if condition {
		id, err = NewID(raw)
	} else {
		err = errors.New("failure")
	}
	return id, err
}
