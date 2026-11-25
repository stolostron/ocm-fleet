// Package optional represents values that may be absent without using their
// zero values as absence sentinels.
package optional

// Optional holds a value or absence. Its zero value is absent.
//
// Values are stored inline and copied with T's ordinary shallow-copy semantics.
// Optional does not clone mutable backing data or validate domain invariants.
// It has no native serialization contract; map presence into a boundary DTO
// instead of encoding its private storage.
type Optional[T any] struct {
	value   T
	present bool
}

// Some holds a copy of value, which must satisfy T's invariants. A valid zero or
// nil value is present; only Optional's zero value represents absence.
func Some[T any](value T) Optional[T] {
	return Optional[T]{value: value, present: true}
}

// Get returns a copy of the value and whether it is present. When absent, the
// returned value is zero and must not be used where a valid T is required.
//
//domain:presence
func (o Optional[T]) Get() (T, bool) {
	return o.value, o.present
}
