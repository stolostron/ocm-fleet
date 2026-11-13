package values

import (
	"errors"
	"reflect"
)

// ID is a non-empty identifier without slashes.
//
//domain:constructors NewID RestoreID
type ID string // want ID:"requires constructors"

func NewID(raw string) (ID, error) {
	_ = func(raw string) ID {
		return ID(raw) // want "construct values.ID through NewID or RestoreID"
	}
	_ = Ref{key: raw} // want "construct values.Ref through NewRef or Alias.Ref"
	if raw == "" {
		return "", errors.New("empty identifier")
	}
	return ID(raw), nil
}

// RestoreID stands in for a root's compatibility-aware hydration helper.
func RestoreID(raw string) ID { return ID(raw) }

// Count is positive.
//
//domain:constructors NewCount
type Count int // want Count:"requires constructors"

func NewCount(raw int) (Count, error) {
	if raw <= 0 {
		return 0, errors.New("nonpositive count")
	}
	return Count(raw), nil
}

// Ref has two required components.
//
//domain:constructors NewRef Alias.Ref
type Ref struct { // want Ref:"requires constructors"
	namespace string
	key       string
}

func NewRef(namespace, key string) (Ref, error) {
	if namespace == "" || key == "" {
		return Ref{}, errors.New("missing component")
	}
	return Ref{namespace: namespace, key: key}, nil
}

func (r Ref) Key() string { return r.key }

type Alias struct{ namespace, key string }

func (a Alias) Ref() Ref { return Ref{namespace: a.namespace, key: a.key} }

// Total has a valid zero but still requires construction for other values.
//
//domain:constructors NewTotal
//domain:zero-valid
type Total int // want Total:"requires constructors"

func NewTotal(raw int) Total { return Total(raw) }

// Empty has a useful empty state.
//
//domain:constructors NewEmpty
//domain:zero-valid
type Empty struct{ value string } // want Empty:"requires constructors"

func NewEmpty(raw string) Empty { return Empty{value: raw} }

// Ordinary typed constants remain appropriate for enums.
type State string

const Ready State = "ready"

func internalBypasses(raw string, id ID, ref Ref, count Count) {
	_ = ID(raw)                  // want "construct values.ID through NewID or RestoreID"
	var literal ID = "unchecked" // want "construct values.ID through NewID or RestoreID"
	_ = literal
	_ = id + ID("suffix")             // want "construct values.ID through NewID or RestoreID"
	count++                           // want "construct values.Count through NewCount"
	count *= 2                        // want "construct values.Count through NewCount"
	_ = -count                        // want "construct values.Count through NewCount"
	ref.key = raw                     // want "modify values.Ref through NewRef or Alias.Ref"
	_ = &ref.key                      // want "expose mutable contents of values.Ref"
	_ = Ref{namespace: raw, key: raw} // want "construct values.Ref through NewRef or Alias.Ref"
	_ = Ref{}
}

func NewUnapproved(raw string) ID {
	return ID(raw) // want "construct values.ID through NewID or RestoreID"
}

// Approval is scoped to the named type, not the whole constructor body.
func (*Alias) NewRef(raw string) Ref {
	return Ref{key: raw} // want "construct values.Ref through NewRef or Alias.Ref"
}

func errorsAndStorage(raw string, err error) (Ref, error) {
	var storage Ref
	storage, err = NewRef(raw, raw)
	if err != nil {
		return Ref{}, err
	}
	return storage, nil
}

func invalidSuccess() (Ref, error) {
	return Ref{}, nil
}

func invalidScalarSuccess() (ID, error) {
	return "", nil
}

func permittedCopies(id ID, ref Ref, count Count) {
	copy := id
	_ = ID(copy)
	_ = string(copy)
	_ = id == copy
	_ = map[ID]Ref{id: ref}
	_ = []Count{count}
	var storage ID
	storage = copy
	_ = storage
	var zero Total = 0
	_ = zero
	_ = Empty{}
	_ = Ready
	_ = (*ID)(nil)
	_ = (*Ref)(nil)
}

func invalidNonzero() Total {
	return 42 // want "construct values.Total through NewTotal"
}

func (e Empty) InvalidChange(raw string) {
	e.value = raw // want "modify values.Empty through NewEmpty"
}

func (r Ref) InvalidChangeThroughPointer(raw string) {
	pointer := &r
	pointer.key = raw // want "modify values.Ref through NewRef or Alias.Ref"
}

func typeMetadata() reflect.Type { return reflect.TypeOf(Ref{}) }

func RawCast[T ~string](raw string) T { return T(raw) }

func GenericBypass(raw string) ID {
	return RawCast[ID](raw) // want "construct values.ID through NewID or RestoreID"
}
