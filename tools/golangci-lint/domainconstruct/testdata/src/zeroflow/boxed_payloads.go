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
