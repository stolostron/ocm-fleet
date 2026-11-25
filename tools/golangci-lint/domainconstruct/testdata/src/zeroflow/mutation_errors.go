package zeroflow

import "errors"

func JoinedErrorUnaffectedByOtherRange(mutate func([]error)) (ID, error) {
	errs := []error{nil, errors.New("failure")}
	mutate(errs[:1:1])
	return "", errors.Join(errs[1:]...)
}

func JoinedErrorMayChangeThroughCapacity(mutate func([]error)) (ID, error) {
	errs := []error{nil, errors.New("failure")}
	mutate(errs[:1])
	return "", errors.Join(errs[1:]...) // want "return produces an empty zeroflow.ID"
}

func inspectErrors(errs []error) int { return len(errs) }

func JoinedErrorAfterReadOnlyHelper() (ID, error) {
	errs := []error{errors.New("failure")}
	_ = inspectErrors(errs)
	return "", errors.Join(errs...)
}

func JoinedErrorAfterPointerMutation(invoke func(...any)) (ID, error) {
	errs := []error{errors.New("failure")}
	invoke(&errs[0])
	return "", errors.Join(errs...) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func JoinedErrorAfterCapturedMutation(invoke func(func())) (ID, error) {
	errs := []error{errors.New("failure")}
	invoke(func() { errs[0] = nil })
	return "", errors.Join(errs...) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func JoinedErrorAfterMutation(mutate func([]error)) (ID, error) {
	errs := []error{errors.New("failure")}
	mutate(errs)
	return "", errors.Join(errs...) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

type errorBox struct{ entries map[int][]error }

func clearBoxedErrors(box *errorBox) { clear(box.entries[0]) }

func JoinedErrorAfterMutationThroughMapField() (ID, error) {
	errs := make([]error, 1)
	box := &errorBox{entries: map[int][]error{0: errs}}
	errs[0] = errors.New("failure")
	clearBoxedErrors(box)
	return "", errors.Join(errs...) // want "return produces an empty zeroflow.ID"
}

func JoinedErrorAfterDisjointMapFieldMutation() (ID, error) {
	errs := make([]error, 1)
	other := []error{errors.New("other")}
	box := &errorBox{entries: map[int][]error{0: other}}
	errs[0] = errors.New("failure")
	clearBoxedErrors(box)
	return "", errors.Join(errs...)
}

func clearReferencedError(refs []*error) { *refs[0] = nil }

func JoinedErrorAfterMutationThroughAppendedPointer() (ID, error) {
	errs := make([]error, 1)
	other := errors.New("other")
	refs := make([]*error, 1, 2)
	refs[0] = &other
	_ = append(refs, &errs[0])
	errs[0] = errors.New("failure")
	clearReferencedError(refs[1:2])
	return "", errors.Join(errs...) // want "return produces an empty zeroflow.ID"
}

func JoinedErrorAfterMutationOfDisjointAppendRange() (ID, error) {
	errs := make([]error, 1)
	other := errors.New("other")
	refs := make([]*error, 1, 2)
	refs[0] = &other
	_ = append(refs, &errs[0])
	errs[0] = errors.New("failure")
	clearReferencedError(refs[:1:1])
	return "", errors.Join(errs...)
}

func JoinedErrorAfterAppendAllocates() (ID, error) {
	errs := make([]error, 1)
	other := errors.New("other")
	refs := []*error{&other}
	_ = append(refs[:0:0], &errs[0])
	errs[0] = errors.New("failure")
	clearReferencedError(refs)
	return "", errors.Join(errs...)
}
