package zeromutator

import "zeroflow"

func ReassignSentinel() {
	zeroflow.ErrMissing = nil // want "do not reassign.*error sentinel ErrMissing"
}

func ExposeSentinel() *error {
	return &zeroflow.ErrAlias // want "do not reassign or expose.*error sentinel ErrAlias"
}

func MutatedSentinelFailure() (zeroflow.Ref, error) {
	return zeroflow.Ref{}, zeroflow.ErrMissing // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}

func MutatedError() error { return zeroflow.ErrMissing }

func MutatedWrapperFailure() (zeroflow.Ref, error) {
	return zeroflow.Ref{}, MutatedError() // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]Ref\""
}
