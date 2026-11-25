package optionalhelpers

import "github.com/stolostron/ocm-fleet/server/pkg/optional"

func Absent[T any]() optional.Optional[T] { return optional.Optional[T]{} }

func NamedAbsent[T any]() (value optional.Optional[T]) { return }

func Wrap[T any](value T) optional.Optional[T] { return optional.Some(value) }

func PresentZero[T any]() optional.Optional[T] {
	var zero T
	return optional.Some(zero)
}

func MaybeZero[T any](found bool) optional.Optional[T] {
	if !found {
		return optional.Optional[T]{}
	}
	return PresentZero[T]()
}

func Construct[T ~string](raw string) optional.Optional[T] {
	return optional.Some(T(raw))
}

//domain:presence
func Forward[T any](value optional.Optional[T]) (T, bool) { return value.Get() }
