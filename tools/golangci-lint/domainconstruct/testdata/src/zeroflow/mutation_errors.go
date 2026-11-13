package zeroflow

import "errors"

func JoinedErrorUnaffectedByOtherRange(mutate func([]error)) (ID, error) {
	errs := []error{nil, errors.New("failure")}
	mutate(errs[:1])
	return "", errors.Join(errs[1:]...)
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
