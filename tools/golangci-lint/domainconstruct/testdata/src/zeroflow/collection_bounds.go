package zeroflow

import "errors"

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
