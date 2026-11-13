package zeroflow

import "errors"

func Zero[T any]() (value T) { return }

func LocalZero[T any]() T {
	var value T
	return value
}

func IndirectZero[T any]() T { return *new(T) }

func FreshPointer[T any]() *T { return new(T) }

func FreshSlice[T any](size int) []T { return make([]T, size) }

func Identity[T any](value T) T { return value }

func ForwardZero[T any]() T { return LocalZero[T]() }

func ChooseZero[T any](value T, empty bool) T {
	if empty {
		var zero T
		return zero
	}
	return value
}

type GenericBox[T any] struct{ Value T }

func (GenericBox[T]) Zero() (value T) { return }

func (box GenericBox[T]) Get() T { return box.Value }

func Box[T any](value T) GenericBox[T] { return GenericBox[T]{Value: value} }

func GenericZeros(valid ID, empty bool) {
	Accept(Zero[ID]())               // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	Accept(LocalZero[ID]())          // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	Accept(IndirectZero[ID]())       // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	Accept(ForwardZero[ID]())        // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	Accept(ChooseZero(valid, empty)) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	AcceptTotal(Zero[Total]())
	AcceptPointer(Zero[*ID]())
}

func GenericCopies(valid ID) {
	Accept(Identity(valid))
	Accept(Identity(Zero[ID]())) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	var zero ID
	Accept(Identity(zero)) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func GenericPointers(valid ID) {
	AcceptPointer(FreshPointer[ID]()) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	id := FreshPointer[ID]()
	*id = valid
	AcceptPointer(id)
}

func GenericSlices(size int, valid ID) {
	AcceptIDs(FreshSlice[ID](size)) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	AcceptIDs(FreshSlice[ID](0))
	ids := FreshSlice[ID](size)
	for index := range ids {
		ids[index] = valid
	}
	AcceptIDs(ids)
}

func GenericStructs(valid ID) {
	AcceptAny(Zero[GenericBox[ID]]()) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	Accept(Box(valid).Get())
	Accept(GenericBox[ID]{}.Get())              // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	Accept(GenericBox[ID]{Value: valid}.Zero()) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	AcceptAny(Box(Zero[ID]()))                  // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func GenericErrorZero[T any]() (T, error) {
	var zero T
	return zero, nil
}

func GenericCheckedResult() {
	id, err := GenericErrorZero[ID]()
	if err == nil {
		Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	}
}

func GenericErrorPayload[T any](valid T, fail bool) (T, error) {
	if fail {
		var zero T
		return zero, errors.New("failure")
	}
	return valid, nil
}

func GenericCheckedPayload(valid ID, fail bool) {
	id, err := GenericErrorPayload(valid, fail)
	if err == nil {
		Accept(id)
	}
}

func FixedSlice[T any]() []T { return make([]T, 2) }

func ForwardSlice[T any](size int) []T { return FreshSlice[T](size) }

func GenericContainers(valid ID) {
	AcceptIDs(FixedSlice[ID]()) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	AcceptIDs(ForwardSlice[ID](0))
	AcceptIDs(ForwardSlice[ID](2)) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	ids := FreshSlice[ID](2)
	ids[0], ids[1] = valid, valid
	AcceptIDs(ids)
	AcceptAny(Zero[[2]ID]()) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func GenericCleared(valid ID) {
	ids := FreshSlice[ID](1)
	ids[0] = valid
	clear(ids)
	AcceptIDs(ids) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func MaybePointer[T any](present bool) *T {
	if present {
		return new(T)
	}
	return nil
}

func MaybeSlice[T any](present bool) []T {
	if present {
		return make([]T, 2)
	}
	return nil
}

func SliceWithError[T any](size int) ([]T, error) { return make([]T, size), nil }

func GenericOptionalAllocations(present bool) {
	AcceptPointer(MaybePointer[ID](present)) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	AcceptIDs(MaybeSlice[ID](present))       // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	ids, err := SliceWithError[ID](2)
	if err == nil {
		AcceptIDs(ids) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	}
}

func GenericOptionalFill(present bool, valid ID) {
	ids := MaybeSlice[ID](present)
	for index := range ids {
		ids[index] = valid
	}
	AcceptIDs(ids)
}
