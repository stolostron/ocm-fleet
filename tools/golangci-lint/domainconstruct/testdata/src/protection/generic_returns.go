package protection

import "errors"

func castPresent[T ~int](raw int) (T, bool) { return T(raw), true }

//domain:presence
func forwardPresence[T ~int](raw int) (T, bool) { return castPresent[T](raw) }

func ForwardedConstruction(raw int) {
	_, _ = forwardPresence[Count](raw) // want "construct protection.Count through NewCount"
}

func useRejected[T ~int](raw int) (T, error) {
	return T(raw), errors.New("failure")
}

func forwardRejected[T ~int](raw int) (T, error) {
	return useRejected[T](raw)
}

func zeroOnFailure[T ~int]() (T, error) { return T(0), errors.New("failure") }

func FailureDoesNotAuthorizeConstruction(raw int) {
	_, _ = useRejected[Count](raw)     // want "construct protection.Count through NewCount"
	_, _ = forwardRejected[Count](raw) // want "construct protection.Count through NewCount"
	_, _ = zeroOnFailure[Count]()
}
