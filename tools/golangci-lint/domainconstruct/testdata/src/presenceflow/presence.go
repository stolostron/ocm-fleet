// Package presenceflow exercises explicit boolean presence contracts.
package presenceflow

import (
	"errors"
	"fmt"
	"iter"
	"testing"
)

//domain:constructors NewID
type ID string

func NewID(raw string) ID {
	if raw == "" {
		panic("empty identifier")
	}
	return ID(raw)
}

//domain:constructors NewTotal
//domain:zero-valid
type Total int

func NewTotal(raw int) Total { return Total(raw) }

type Record struct{ ID ID }

type Registry map[string]ID

type Provider interface {
	//domain:presence
	Lookup(string) (ID, bool, error)
}

type EmbeddedProvider interface{ Provider }

type Creator interface {
	Lookup(string) (ID, bool, error)
}

func Use(ID)           {}
func UseRecord(Record) {}
func UsePointer(*ID)   {}
func UseAny(any)       {}
func UseSlice([]ID)    {}
func UseTotal(Total)   {}

//domain:presence
func Lookup(found bool) (ID, bool) {
	if !found {
		return "", false
	}
	return NewID("present"), true
}

//domain:presence
func LookupError(found bool, err error) (ID, bool, error) {
	if err != nil {
		return "", true, err
	}
	if !found {
		return "", false, nil
	}
	return NewID("present"), true, nil
}

//domain:presence
func (r Registry) Lookup(key string) (ID, bool, error) {
	id, ok := r[key]
	return id, ok, nil
}

//domain:presence
func MissingNamedResult() (id ID, ok bool) { return }

//domain:presence
func InvalidPresent() (ID, bool) {
	return "", true // want "(?s)rule: domainzero[.]invalid-zero.*operation: return"
}

//domain:presence
func InvalidSuccess() (ID, bool, error) {
	return "", true, nil // want "(?s)rule: domainzero[.]invalid-zero.*operation: return"
}

//domain:presence
func UnknownPresence(found bool) (ID, bool) {
	return "", found // want "(?s)rule: domainzero[.]invalid-zero.*operation: return"
}

//domain:presence
func UnknownError(err error) (ID, bool, error) {
	return "", true, err // want "(?s)rule: domainzero[.]invalid-zero.*operation: return"
}

//domain:presence
func CompositeMissing() (Record, bool) { return Record{}, false }

//domain:presence
func CompositePresent() (Record, bool) {
	return Record{}, true // want "(?s)rule: domainzero[.]invalid-zero.*path: \"ID\""
}

//domain:presence
func PointerMissing() (*ID, bool) {
	var id ID
	return &id, false
}

//domain:presence
func PointerPresent() (*ID, bool) {
	var id ID
	return &id, true // want "(?s)rule: domainzero[.]invalid-zero.*operation: return"
}

//domain:presence
func BoxedMissing() (any, bool) { return ID(""), false }

//domain:presence
func BoxedPresent() (any, bool) {
	return ID(""), true // want "(?s)rule: domainzero[.]invalid-zero.*operation: return"
}

//domain:presence
func SliceMissing() ([]ID, bool) { return make([]ID, 1), false }

func UnmarkedMissing() (ID, bool) {
	return "", false // want "(?s)rule: domainzero[.]invalid-zero.*operation: return"
}

//domain:presence extra
func InvalidDirective() (ID, bool) { // want "use domain:presence once, without arguments"
	return "", false // want "(?s)rule: domainzero[.]invalid-zero.*operation: return"
}

//domain:presence
func NestedClosure() (ID, bool) {
	lookup := func() (ID, bool) {
		return "", false // want "(?s)rule: domainzero[.]invalid-zero.*operation: return"
	}
	return lookup()
}

func GetOrCreate() (ID, bool, error) { return NewID("existing"), false, nil }

func OtherBooleanMeaning() {
	id, _, err := GetOrCreate()
	if err == nil {
		Use(id)
	}
}

func Checked(found bool) {
	id, ok := Lookup(found)
	if ok {
		Use(id)
	}
}

func CheckedEarlyReturn(found bool) {
	id, ok := Lookup(found)
	if !ok {
		return
	}
	Use(id)
}

func CheckedCopiedFlag(found bool) {
	id, ok := Lookup(found)
	present := ok
	if present == true {
		Use(id)
	}
}

func FunctionAlias(found bool) {
	lookup := Lookup
	id, ok := lookup(found)
	if ok {
		Use(id)
	}
}

func UncheckedFunctionAlias(found bool) {
	lookup := Lookup
	id, _ := lookup(found)
	Use(id) // want "reason: unchecked-presence"
}

func CheckedFatal(t testing.TB, found bool) {
	id, ok := Lookup(found)
	if !ok {
		t.Fatal("missing")
	}
	Use(id)
}

func CheckedBoth(found bool, err error) {
	id, ok, err := LookupError(found, err)
	if err == nil && ok {
		Use(id)
	}
}

func CheckedBothReversed(found bool, err error) {
	id, ok, err := LookupError(found, err)
	if ok && err == nil {
		Use(id)
	}
}

func CheckedBothEarlyReturn(found bool, err error) {
	id, ok, err := LookupError(found, err)
	if err != nil || !ok {
		return
	}
	Use(id)
}

func Unchecked(found bool) {
	id, _ := Lookup(found)
	Use(id) // want "reason: unchecked-presence"
}

func CheckedOnlyError(found bool, err error) {
	id, _, err := LookupError(found, err)
	if err == nil {
		Use(id) // want "reason: unchecked-presence"
	}
}

func CheckedOnlyPresence(found bool, err error) {
	id, ok, _ := LookupError(found, err)
	if ok {
		Use(id) // want "reason: unchecked-error"
	}
}

func UsedOnMissingPath(found bool) {
	id, ok := Lookup(found)
	if !ok {
		Use(id) // want "reason: unchecked-presence"
	}
}

func UsedAfterJoin(found bool) {
	id, ok := Lookup(found)
	if ok {
		Use(id)
	}
	Use(id) // want "reason: unchecked-presence"
}

func NonfatalCheck(t *testing.T, found bool) {
	id, ok := Lookup(found)
	if !ok {
		t.Error("missing")
	}
	Use(id) // want "reason: unchecked-presence"
}

func UnrelatedFlag(found bool) {
	id, _ := Lookup(found)
	_, ok := Lookup(found)
	if ok {
		Use(id) // want "reason: unchecked-presence"
	}
}

func ReassignedFlag(found bool) {
	id, ok := Lookup(found)
	ok = true
	if ok {
		Use(id) // want "reason: unchecked-presence"
	}
}

func ReassignedValue(found bool) {
	id, ok := Lookup(found)
	if ok {
		id = ""
		Use(id) // want "reason: zero-value"
	}
}

func WrongError(found bool, err error) {
	id, ok, _ := LookupError(found, err)
	_, _, other := LookupError(found, err)
	if ok && other == nil {
		Use(id) // want "reason: unchecked-error"
	}
}

func UncheckedComposite() {
	record, _ := CompositeMissing()
	UseRecord(record) // want "(?s)reason: unchecked-presence.*path: \"ID\""
}

func UncheckedPointer() {
	id, _ := PointerMissing()
	UsePointer(id) // want "reason: unchecked-presence"
}

func UncheckedBoxed() {
	id, _ := BoxedMissing()
	UseAny(id) // want "reason: unchecked-presence"
}

func UncheckedSlice() {
	ids, _ := SliceMissing()
	UseSlice(ids) // want "reason: unchecked-presence"
}

//domain:presence
func LookupTotal() (Total, bool) { return 0, false }

func ValidZeroNeedsNoGuard() {
	total, _ := LookupTotal()
	UseTotal(total)
}

//domain:presence
func Forward(found bool) (ID, bool) { return Lookup(found) }

//domain:presence
func ForwardError(found bool, err error) (ID, bool, error) {
	id, ok, err := LookupError(found, err)
	return id, ok, err
}

//domain:presence
func ForwardMissingFlag(found bool) (ID, bool) {
	id, _ := Lookup(found)
	return id, true // want "reason: unchecked-presence"
}

func ForwardAsRequired(found bool) ID {
	id, _ := Lookup(found)
	return id // want "reason: unchecked-presence"
}

//domain:presence
func ForwardWrongError(found bool, err error) (ID, bool, error) {
	id, ok, _ := LookupError(found, err)
	return id, ok, nil // want "reason: unchecked-error"
}

//domain:presence
func WrappedFailure(err error) (ID, bool, error) {
	if err != nil {
		return "", false, fmt.Errorf("lookup: %w", err)
	}
	return NewID("present"), true, nil
}

//domain:presence
func KnownFailure() (ID, bool, error) { return "", true, errors.New("failed") }

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("lookup: %w", err)
}

//domain:presence
func ForwardWrapped(found bool, err error) (ID, bool, error) {
	id, ok, err := LookupError(found, err)
	return id, ok, wrap(err)
}

func ConcreteMethod(registry Registry, key string) {
	id, ok, err := registry.Lookup(key)
	if err == nil && ok {
		Use(id)
	}
}

func UncheckedConcreteMethod(registry Registry, key string) {
	id, _, err := registry.Lookup(key)
	if err == nil {
		Use(id) // want "reason: unchecked-presence"
	}
}

func MethodExpression(registry Registry, key string) {
	id, ok, err := Registry.Lookup(registry, key)
	if err == nil && ok {
		Use(id)
	}
}

func UncheckedMethodExpression(registry Registry, key string) {
	id, _, err := Registry.Lookup(registry, key)
	if err == nil {
		Use(id) // want "reason: unchecked-presence"
	}
}

func BoundMethod(registry Registry, key string) {
	lookup := registry.Lookup
	id, ok, err := lookup(key)
	if err == nil && ok {
		Use(id)
	}
}

func UncheckedBoundMethod(registry Registry, key string) {
	lookup := registry.Lookup
	id, _, err := lookup(key)
	if err == nil {
		Use(id) // want "reason: unchecked-presence"
	}
}

func InterfaceMethod(provider Provider) {
	id, ok, err := provider.Lookup("key")
	if err == nil && ok {
		Use(id)
	}
}

func UncheckedInterfaceMethod(provider Provider) {
	id, _, err := provider.Lookup("key")
	if err == nil {
		Use(id) // want "reason: unchecked-presence"
	}
}

func UncheckedEmbeddedMethod(provider EmbeddedProvider) {
	id, _, err := provider.Lookup("key")
	if err == nil {
		Use(id) // want "reason: unchecked-presence"
	}
}

func BoundInterfaceMethod(provider Provider) {
	lookup := provider.Lookup
	id, _, err := lookup("key")
	if err == nil {
		Use(id) // want "reason: unchecked-presence"
	}
}

func UnmarkedInterfaceMethod(provider Creator) {
	id, _, err := provider.Lookup("key")
	if err == nil {
		Use(id)
	}
}

//domain:presence
func ForwardInterface(provider Provider) (ID, bool, error) {
	return provider.Lookup("key")
}

//domain:presence
func IteratorMissing(values iter.Seq[int]) (ID, bool) {
	for range values {
		return "", false
	}
	return NewID("present"), true
}

//domain:presence
func IteratorInvalid(values iter.Seq[int]) (ID, bool) {
	for range values {
		return "", true // want "(?s)rule: domainzero[.]invalid-zero.*operation: return"
	}
	return NewID("present"), true
}

//domain:presence
func GenericLookup[T any](value T, found bool) (T, bool) {
	if !found {
		var zero T
		return zero, false
	}
	return value, true
}

//domain:presence
func GenericNamed[T any]() (value T, ok bool) { return }

//domain:presence
func GenericForward[T any](value T, found bool) (T, bool) {
	return GenericLookup(value, found)
}

//domain:presence
func GenericInvalid[T any]() (T, bool) {
	var zero T
	return zero, true
}

func GenericChecked(found bool) {
	id, ok := GenericLookup(NewID("present"), found)
	if ok {
		Use(id)
	}
}

func GenericUnchecked(found bool) {
	id, _ := GenericLookup(NewID("present"), found)
	Use(id) // want "reason: unchecked-presence"
}

func GenericInvalidPresent() {
	id, ok := GenericInvalid[ID]()
	if ok {
		Use(id) // want "reason: zero-value"
	}
}

func GenericForwardChecked(found bool) {
	id, ok := GenericForward(NewID("present"), found)
	if ok {
		Use(id)
	}
}

func GenericNamedUnchecked() {
	id, _ := GenericNamed[ID]()
	Use(id) // want "reason: unchecked-presence"
}
