package zeroflow

import "errors"

//domain:constructors NewTruncatedCount
type TruncatedCount int

func NewTruncatedCount(small bool) TruncatedCount {
	if small {
		return truncateFloat[TruncatedCount](0.5) // want "return produces a zero zeroflow.TruncatedCount"
	}
	return truncateFloat[TruncatedCount](1.5)
}

func truncateFloat[T ~int](value float64) T { return T(value) }

func allocatedOrNil[T any](absent bool) []T {
	if absent {
		return nil
	}
	return make([]T, 1, 2)
}

func CheckCapacityAfterNilAlternative(absent bool, index int, use func([]ID)) {
	items := allocatedOrNil[ID](absent)
	use(items[index:index][:1]) // want "argument may contain an empty zeroflow.ID"
}

func CopyFromOptionalAllocation(valid ID, absent bool) []ID {
	source := allocatedOrNil[ID](absent)
	copy(source, []ID{valid})
	destination := make([]ID, 1)
	copy(destination, source)
	return destination // want "return may contain an empty zeroflow.ID"
}

func CopyBoxFromOptionalAllocation(absent bool) []any {
	source := allocatedOrNil[any](absent)
	copy(source, []any{"valid"})
	destination := []any{ID("")}
	copy(destination, source)
	return destination // want "return .* an empty zeroflow.ID"
}

func FillOptionalAllocation(valid ID, absent bool) []ID {
	source := allocatedOrNil[ID](absent)
	copy(source, []ID{valid})
	return source
}

func CopyOptionalAllocationBeforeReslice(valid ID, absent bool) []ID {
	source := allocatedOrNil[ID](absent)
	copy(source, []ID{valid})
	return source[:2] // want "return may contain an empty zeroflow.ID"
}

func ReadFieldAfterCopyFromOptionalAllocation(valid ID, absent bool, use func(ID)) {
	source := allocatedOrNil[projectedRow](absent)
	copy(source, []projectedRow{{ID: valid}})
	destination := make([]projectedRow, 1)
	copy(destination, source)
	use(destination[0].ID) // want "argument .* an empty zeroflow.ID"
}

func allocateOnSuccess[T any](fail bool) ([]T, error) {
	if fail {
		return nil, errors.New("unavailable")
	}
	return make([]T, 1), nil
}

func CopyFromUncheckedAllocation(valid ID, fail bool) []ID {
	source, _ := allocateOnSuccess[ID](fail)
	copy(source, []ID{valid})
	destination := make([]ID, 1)
	copy(destination, source[:])
	return destination // want "return may contain an empty zeroflow.ID"
}

func CopyFromCheckedAllocation(valid ID, fail bool) ([]ID, error) {
	source, err := allocateOnSuccess[ID](fail)
	if err != nil {
		return nil, err
	}
	copy(source, []ID{valid})
	destination := make([]ID, 1)
	copy(destination, source[:])
	return destination, nil
}

func ReadFieldAfterCopyFromUncheckedAllocation(valid ID, fail bool, use func(ID)) {
	source, _ := allocateOnSuccess[projectedRow](fail)
	copy(source, []projectedRow{{ID: valid}})
	destination := make([]projectedRow, 1)
	copy(destination, source)
	use(destination[0].ID) // want "argument may be an empty zeroflow.ID"
}

func allocateOnEitherResult[T any](fail bool) ([]T, error) {
	if fail {
		return make([]T, 1), errors.New("unavailable")
	}
	return make([]T, 1), nil
}

func CopyFromAllReturnLength(valid ID, fail bool) []ID {
	source, _ := allocateOnEitherResult[ID](fail)
	copy(source, []ID{valid})
	destination := make([]ID, 1)
	copy(destination, source)
	return destination
}

//domain:presence
func allocateWhenPresent[T any](found bool) ([]T, bool) {
	if !found {
		return nil, false
	}
	return make([]T, 1), true
}

func CopyWithoutCheckingPresence(valid ID, found bool) []ID {
	source, _ := allocateWhenPresent[ID](found)
	copy(source, []ID{valid})
	destination := make([]ID, 1)
	copy(destination, source)
	return destination // want "return may contain an empty zeroflow.ID"
}

func CopyAfterCheckingPresence(valid ID, found bool) []ID {
	source, present := allocateWhenPresent[ID](found)
	if !present {
		return nil
	}
	copy(source, []ID{valid})
	destination := make([]ID, 1)
	copy(destination, source)
	return destination
}

func allocatedOneOrTwo[T any](short bool) []T {
	if short {
		return make([]T, 1)
	}
	return make([]T, 2)
}

func CopyGuaranteedPrefix(valid ID, short bool) []ID {
	source := allocatedOneOrTwo[ID](short)
	copy(source, []ID{valid, valid})
	destination := make([]ID, 1)
	copy(destination, source)
	return destination
}

func CopyPartiallyWrittenChoice(valid ID, short bool) []ID {
	source := allocatedOneOrTwo[ID](short)
	copy(source, []ID{valid})
	destination := []ID{valid, valid}
	copy(destination, source)
	return destination // want "return may contain an empty zeroflow.ID"
}

func AppendOptionalAllocationIntoSpareCapacity(valid ID, absent bool) []ID {
	values := []ID{valid, valid}
	_ = append(values[:0], allocatedOrNil[ID](absent)...)
	return values // want "return .* an empty zeroflow.ID"
}

func AppendInitializedOptionalAllocation(valid ID, absent bool) []ID {
	values := make([]ID, 0, 2)
	source := allocatedOrNil[ID](absent)
	copy(source, []ID{valid})
	return append(values, source...)
}

func allocatedFromEitherLength[T any](first, second []T, chooseFirst bool) []T {
	if chooseFirst {
		return make([]T, len(first))
	}
	return make([]T, len(second))
}

func CheckAlternativeLengthBases(n int, chooseFirst bool, valid ID, use func([]ID)) {
	backing := []ID{valid, valid}
	if n == 1 {
		use(allocatedFromEitherLength(backing[1:n], backing[:n], chooseFirst)) // want "argument may contain an empty zeroflow.ID"
		result := allocatedFromEitherLength(backing[1:n], backing[:n], chooseFirst)
		use(result[:]) // want "argument may contain an empty zeroflow.ID"
	}
}

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
	Accept(ChooseZero(valid, empty)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible;.*type: \"zeroflow[.]ID\""
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
