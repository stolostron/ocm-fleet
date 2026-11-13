package zeroflow

import (
	"errors"
	"reflect"
)

func ZeroLookupKey(values map[ID]int) int {
	var id ID
	return values[id] // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func ZeroComparison(valid ID) bool {
	var id ID
	return id == valid // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func PointerPresence() bool {
	id := new(ID)
	return id != nil
}

func ZeroMethodValue() string {
	var ref Ref
	get := ref.Key
	return get() // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func EscapingZeroMethod() func() string {
	var ref Ref
	return ref.Key // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func ValidMethodValue(ref Ref) string {
	get := ref.Key
	return get()
}

func SelectReceive(ch <-chan ID) {
	select {
	case id := <-ch:
		Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	default:
	}
}

func CheckedSelectReceive(first, second <-chan ID) {
	select {
	case id, ok := <-first:
		if ok {
			Accept(id)
		}
	case id, ok := <-second:
		if !ok {
			return
		}
		Accept(id)
	default:
	}
}

func FailedSelectReceive(ch <-chan ID) {
	select {
	case id, ok := <-ch:
		if !ok {
			Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
		}
	default:
	}
}

func SelectSend(ch chan<- ID) {
	var id ID
	select {
	case ch <- id: // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	default:
	}
}

func TypeMetadata() reflect.Type { return reflect.TypeOf(Ref{}) }

func BoxedFailure() (any, error) {
	var ref Ref
	return ref, errors.New("failure")
}

func StoredBoxedFailure() (any, error) {
	var ref Ref
	var payload any = ref
	return payload, errors.New("failure")
}

func BoxedSuccess() any {
	var ref Ref
	return ref // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func BoxedArgument() {
	var ref Ref
	var payload any = ref
	AcceptAny(payload) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func BoxedTemporary(valid ID) {
	var id ID
	var payload any = id
	payload = valid
	AcceptAny(payload)
}

func BoxedAssertion() {
	var id ID
	var payload any = id
	Accept(payload.(ID)) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func PointerFailure() (*ID, error) { return new(ID), errors.New("failure") }

func UncheckedPointerResult() {
	id, _ := PointerFailure()
	AcceptPointer(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func CheckedPointerResult() error {
	id, err := PointerFailure()
	if err != nil {
		return err
	}
	AcceptPointer(id)
	return nil
}

func ForwardPointerResult() (*ID, error) { return PointerFailure() }

func UncheckedBoxedResult() {
	payload, _ := BoxedFailure()
	AcceptAny(payload) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func UncheckedStoredBoxedResult() {
	payload, _ := StoredBoxedFailure()
	AcceptAny(payload) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func CheckedBoxedResult() error {
	payload, err := StoredBoxedFailure()
	if err != nil {
		return err
	}
	AcceptAny(payload)
	return nil
}

func OptionalPointerLookup(values map[string]*ID) { AcceptPointer(values["missing"]) }

func OptionalPointerReceive(values <-chan *ID) { AcceptPointer(<-values) }

// StagedName can be inspected during its own construction.
//
//domain:constructors NewStagedName
type StagedName string

func NewStagedName(raw string) (StagedName, error) {
	name := StagedName(raw)
	if name == "" {
		return "", ErrMissing
	}
	return name, nil
}

// BrokenName still cannot escape successfully with an invalid zero.
//
//domain:constructors NewBrokenName
type BrokenName string

func NewBrokenName() (BrokenName, error) {
	return "", nil // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]BrokenName\""
}
