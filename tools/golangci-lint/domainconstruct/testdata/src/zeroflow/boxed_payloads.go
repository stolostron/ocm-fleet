package zeroflow

import (
	"encoding/json"
	"errors"
)

func successfulBox(value ID) (any, error) { return value, nil }

func forwardSuccessfulBox(value ID) (any, error) {
	payload, err := successfulBox(value)
	return payload, err
}

func failAfterBoxing(value ID) (any, error) {
	payload, _ := successfulBox(value)
	return payload, errors.New("failure")
}

func independentBoxStatus(value ID, err error) (any, error) {
	payload, _ := successfulBox(value)
	return payload, err
}

func mixedBox(value ID, fail bool) (any, error) {
	if fail {
		return "no ID", errors.New("failure")
	}
	return value, nil
}

func directBoxForward(value ID, fail bool) (any, error) {
	return mixedBox(value, fail)
}

func boundBoxForward(value ID, fail bool) (any, error) {
	payload, err := mixedBox(value, fail)
	return payload, err
}

func replacedBoxStatus(value ID, fail bool, replacement error) (any, error) {
	payload, err := mixedBox(value, fail)
	if replacement != nil {
		err = replacement
	}
	return payload, err
}

func UseIndependentBoxStatus(value ID, fail bool, err error, use func(any)) {
	payload, _ := independentBoxStatus(value, err)
	use(payload) // want "argument may be an empty zeroflow.ID"
	payload, _ = directBoxForward(value, fail)
	use(payload)
	payload, _ = boundBoxForward(value, fail)
	use(payload)
	payload, _ = replacedBoxStatus(value, fail, err)
	use(payload) // want "argument may be an empty zeroflow.ID"
}

func UseFailureAfterBoxing(value ID, use func(any)) {
	payload, _ := failAfterBoxing(value)
	use(payload) // want "argument may be an empty zeroflow.ID"
	direct, _ := successfulBox(value)
	use(direct)
	forwarded, _ := forwardSuccessfulBox(value)
	use(forwarded)
}

func replacedLoopBoxStatus(value ID) (any, error) {
	payload, err := successfulBox(value)
	for iteration := 0; ; iteration++ {
		if iteration > 0 {
			return payload, err
		}
		err = errors.New("failure")
	}
}

func unchangedLoopBoxStatus(value ID) (any, error) {
	payload, err := successfulBox(value)
	for iteration := 0; ; iteration++ {
		if iteration > 0 {
			return payload, err
		}
	}
}

func UseLoopBoxStatus(value ID, use func(any)) {
	payload, _ := replacedLoopBoxStatus(value)
	use(payload) // want "argument may be an empty zeroflow.ID"
	payload, _ = unchangedLoopBoxStatus(value)
	use(payload)
}

func rangeAssignedBox() (any, error) {
	var payload any
	for _, payload = range []ID{""} {
		return payload, errors.New("failure")
	}
	return nil, errors.New("empty input")
}

func rangePreservesInitialBox(keys []string) (any, error) {
	var payload any = ID("")
	for _, payload = range keys {
	}
	return payload, errors.New("failure")
}

func UseRangeAssignedBox(use func(any)) {
	payload, _ := rangeAssignedBox()
	use(payload) // want "argument may be an empty zeroflow.ID"
	payload, _ = rangePreservesInitialBox(nil)
	use(payload) // want "argument may be an empty zeroflow.ID"
	payload, err := rangeAssignedBox()
	if err != nil {
		return
	}
	use(payload)
}

type boxedIDs []ID

func invalidBoxedIDs() (any, error) { return boxedIDs{""}, errors.New("failure") }

type opaqueBoxedIDs []ID

func (opaqueBoxedIDs) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

func invalidOpaqueBoxedIDs() (any, error) { return opaqueBoxedIDs{""}, errors.New("failure") }

func EncodeBoxedNamedSlices() {
	payload, _ := invalidBoxedIDs()
	json.Marshal(payload) // want "argument may be an empty zeroflow.ID"
	opaque, _ := invalidOpaqueBoxedIDs()
	json.Marshal(opaque)
}

type boxedIDArray [1]ID

func invalidBoxedIDArray() (any, error) { return boxedIDArray{""}, errors.New("failure") }

type opaqueBoxedIDArray [1]ID

func (opaqueBoxedIDArray) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

func invalidOpaqueBoxedIDArray() (any, error) { return opaqueBoxedIDArray{""}, errors.New("failure") }

type pointerHookBoxedIDArray [1]ID

func (*pointerHookBoxedIDArray) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

func invalidBoxedIDArrayWithPointerHook() (any, error) {
	return pointerHookBoxedIDArray{""}, errors.New("failure")
}

func opaqueBoxedIDArrayPointer() (any, error) {
	return &pointerHookBoxedIDArray{""}, errors.New("failure")
}

func EncodeBoxedNamedArrays() {
	payload, _ := invalidBoxedIDArray()
	json.Marshal(payload) // want "argument may be an empty zeroflow.ID"
	opaque, _ := invalidOpaqueBoxedIDArray()
	json.Marshal(opaque)
	value, _ := invalidBoxedIDArrayWithPointerHook()
	json.Marshal(value) // want "argument may be an empty zeroflow.ID"
	pointer, _ := opaqueBoxedIDArrayPointer()
	json.Marshal(pointer)
}

type boxedIDMap map[string]ID

func invalidBoxedIDMap() (any, error) {
	return boxedIDMap{"id": ""}, errors.New("failure") // want "map value is an empty zeroflow.ID"
}

type opaqueBoxedIDMap map[string]ID

func (opaqueBoxedIDMap) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

func invalidOpaqueBoxedIDMap() (any, error) {
	return opaqueBoxedIDMap{"id": ""}, errors.New("failure") // want "map value is an empty zeroflow.ID"
}

func EncodeBoxedNamedMaps() {
	payload, _ := invalidBoxedIDMap()
	json.Marshal(payload) // want "argument may be an empty zeroflow.ID"
	opaque, _ := invalidOpaqueBoxedIDMap()
	json.Marshal(opaque)
}
