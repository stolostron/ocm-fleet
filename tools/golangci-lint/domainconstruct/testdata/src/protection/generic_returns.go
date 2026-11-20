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

func boxConversion[T ~int](raw int) any { return any(T(raw)) }
func boxCopy[T ~int](value T) any       { return T(value) }

func forwardBoxConversion[T ~int](raw int) any {
	return boxConversion[T](raw)
}

func recastField[T ~int, U ~int](input *struct{ Inner *struct{ Value U } }) T {
	return T(input.Inner.Value)
}

func BoxedConstruction(raw int, value Count, input *struct{ Inner *struct{ Value int } }, typed *struct{ Inner *struct{ Value Count } }) {
	_ = boxConversion[Count](raw)        // want "construct protection.Count through NewCount"
	_ = forwardBoxConversion[Count](raw) // want "construct protection.Count through NewCount"
	_ = boxConversion[int](raw)
	_ = boxCopy[Count](value)
	_ = recastField[Count](input) // want "construct protection.Count through NewCount"
	_ = recastField[Count](typed)
}
