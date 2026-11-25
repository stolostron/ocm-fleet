package zeroflow

import "errors"

func GuardedCopyLengths(destination []ID, length int) {
	source := make([]ID, length)
	if length == 0 {
		copy(destination[:1], source)
	}
	if length == 1 {
		copy(destination[:1], source) // want "copy source contains an empty zeroflow.ID"
	}
	exactSource := make([]ID, 1)
	if length == 0 {
		copy(destination[:length], exactSource)
	}
	if length == 1 {
		copy(destination[:length], exactSource) // want "copy source contains an empty zeroflow.ID"
	}
}

func WriteAtDynamicOffset(index int, valid ID) []ID {
	values := make([]ID, 2)
	if index != 1 {
		return []ID{valid}
	}
	values[index:index][:1][0] = valid
	return values[:1] // want "return may contain an empty zeroflow.ID"
}

func ReadAtDynamicOffset(index int, valid ID) []ID {
	values := []ID{valid, valid}
	return values[index:index][:1]
}

func CopyAtGuardedOffset(index int, valid ID) []ID {
	values := []ID{valid, valid}
	if index != 1 {
		return values
	}
	copy(values[index:index][:1], make([]ID, 1))
	return values // want "return contains an empty zeroflow.ID at path .1."
}

func CopyAtDisjointGuardedOffset(index int, valid ID) []ID {
	values := []ID{valid, valid}
	if index == 1 {
		copy(values[index:index][:1], make([]ID, 1))
	}
	return values[:1]
}

func CopyEmptyAtShiftedOffset(index int, valid ID) []ID {
	values := []ID{valid, valid}
	if index == 1 {
		copy(values[index:index][:1], []ID{})
	}
	return values
}

func AppendUnknownLengthIntoSpareCapacity(valid ID, count int) []ID {
	values := []ID{valid, valid}
	_ = append(values[:1], make([]ID, count)...)
	return values // want "return may contain an empty zeroflow.ID at path .1."
}

func AppendUnknownLengthWithoutSpareCapacity(valid ID, count int) []ID {
	values := []ID{valid, valid}
	_ = append(values[:1:1], make([]ID, count)...)
	return values
}

func ReadFieldAfterAppendOfUnknownLength(valid ID, count int) ID {
	values := []projectedRow{{ID: valid}, {ID: valid}}
	_ = append(values[:1], make([]projectedRow, count)...)
	return values[1].ID // want "return may produce an empty zeroflow.ID"
}

func clearToCapacity(values []string) { clear(values[:cap(values)]) }

func CallWithNoAccessibleCapacity(raw string, index int, use func(ID)) {
	values := [1]string{raw}
	if values[0] != "" {
		clearToCapacity(values[index:index:index])
		use(ID(values[0]))
	}
}

func CallWithAccessibleCapacity(raw string, index int, use func(ID)) {
	values := [1]string{raw}
	if values[0] != "" {
		clearToCapacity(values[index:index])
		use(ID(values[0])) // want "argument may be an empty zeroflow.ID"
	}
}

func MakeWithGuardedLength(length int, valid ID) []ID {
	if length == 0 {
		return make([]ID, length)
	}
	if length == 1 {
		return make([]ID, length) // want "return contains an empty zeroflow.ID"
	}
	return []ID{valid}
}

func zerosFor[T any](raw []byte) []T { return make([]T, len(raw)) }

func CopyFromShiftedLength(length int, valid ID) ID {
	raw := make([]byte, length)
	if length != 1 {
		return valid
	}
	destination := []ID{valid}
	copy(destination, zerosFor[ID](raw[1:]))
	return destination[0]
}

func CopyFromFullLength(length int, valid ID) ID {
	raw := make([]byte, length)
	if length != 1 {
		return valid
	}
	destination := []ID{valid}
	copy(destination, zerosFor[ID](raw))
	return destination[0] // want "return contains an empty zeroflow.ID"
}

func JoinAfterDynamicClear(index int, use func(ID)) {
	if index != 1 {
		return
	}
	statuses := []error{errors.New("failure"), nil}
	clear(statuses[index:index][:1])
	if errors.Join(statuses[:1]...) != nil {
		use("") // want "argument is an empty zeroflow.ID"
	}
}

func oldHeader() []byte {
	var header []byte
	header, saved := make([]byte, 1), header
	_ = header
	return saved
}

func FillUsingOldHeader(valid ID) []ID {
	values := make([]ID, 1)
	for index := range oldHeader() {
		values[index] = valid
	}
	return values // want "return may contain an empty zeroflow.ID"
}
