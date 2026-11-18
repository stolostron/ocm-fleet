// Package errorjoins exercises error correlation across local branch merges.
package errorjoins

import "fmt"

//domain:constructors ParseID
type ID string

type Record struct{ ID ID }

type Provider interface {
	//domain:presence
	Lookup() (Record, bool, error)
}

type metadata struct {
	present bool
	err     error
}

func ParseID(raw string) (ID, error) {
	if raw == "" {
		return "", fmt.Errorf("empty identifier")
	}
	return ID(raw), nil
}

func Use(ID)           {}
func UseRecord(Record) {}

func ConditionalWrap(raw string) (ID, error) {
	id, err := ParseID(raw)
	if err != nil {
		err = fmt.Errorf("parse: %w", err)
	}
	return id, err
}

//domain:presence
func ForwardLookup(provider Provider, state *metadata) (Record, bool, error) {
	value, ok, err := provider.Lookup()
	if err != nil {
		err = fmt.Errorf("lookup: %w", err)
	}
	state.present = ok
	state.err = err
	return value, ok, err
}

func CheckedAfterWrap(raw string) {
	id, err := ParseID(raw)
	if err != nil {
		err = fmt.Errorf("parse: %w", err)
	}
	if err == nil {
		Use(id)
	}
}

func CheckedLookup(provider Provider) {
	value, ok, err := provider.Lookup()
	if err != nil {
		err = fmt.Errorf("lookup: %w", err)
	}
	if err != nil || !ok {
		return
	}
	UseRecord(value)
}

func NamedReturn(raw string) (id ID, err error) {
	id, err = ParseID(raw)
	if err != nil {
		err = fmt.Errorf("parse: %w", err)
	}
	return
}

func AliasedError(raw string) (ID, error) {
	id, original := ParseID(raw)
	forwarded := original
	if original != nil {
		forwarded = fmt.Errorf("parse: %w", original)
	}
	return id, forwarded
}

func NestedWrap(raw string, wrap bool) (ID, error) {
	id, err := ParseID(raw)
	if err != nil {
		if wrap {
			err = fmt.Errorf("parse: %w", err)
		}
	}
	return id, err
}

func ConsecutiveWraps(raw string, extra bool) (ID, error) {
	id, err := ParseID(raw)
	if err != nil {
		err = fmt.Errorf("parse: %w", err)
	}
	if extra {
		err = fmt.Errorf("extra: %w", err)
	}
	return id, err
}

func KnownWrapper(raw string) (ID, error) {
	id, err := ParseID(raw)
	if err != nil {
		err = wrapError(err)
	}
	return id, err
}

func ClearedError(raw string) (ID, error) {
	id, err := ParseID(raw)
	if err != nil {
		err = nil
	}
	return id, err // want "reason: unchecked-error"
}

func ReplacedError(raw string, other error) (ID, error) {
	id, err := ParseID(raw)
	if err != nil {
		err = other
	}
	return id, err // want "reason: unchecked-error"
}

func UnrelatedError(raw string, other error) (ID, error) {
	id, _ := ParseID(raw)
	if other != nil {
		other = fmt.Errorf("other: %w", other)
	}
	return id, other // want "reason: unchecked-error"
}

func UnknownWrapper(raw string, wrap func(error) error) (ID, error) {
	id, err := ParseID(raw)
	if err != nil {
		err = wrap(err)
	}
	return id, err // want "reason: unchecked-error"
}

func SwallowedError(raw string, swallow bool) (ID, error) {
	id, err := ParseID(raw)
	if err != nil {
		err = maybeSwallow(err, swallow)
	}
	return id, err // want "reason: unchecked-error"
}

func DifferentCall(raw string) (ID, error) {
	id, _ := ParseID(raw)
	_, err := ParseID(raw)
	if err != nil {
		err = fmt.Errorf("second call: %w", err)
	}
	return id, err // want "reason: unchecked-error"
}

func ClearedInLoop(raw string, flags []bool) (ID, error) {
	id, err := ParseID(raw)
	for _, clear := range flags {
		if clear {
			err = nil
		}
	}
	return id, err // want "reason: unchecked-error"
}

func ReassignedValue(raw string) (ID, error) {
	id, err := ParseID(raw)
	if err != nil {
		err = fmt.Errorf("parse: %w", err)
	}
	id = ""
	return id, err // want "reason: zero-value"
}

func FailedBranch(raw string) {
	id, err := ParseID(raw)
	if err != nil {
		err = fmt.Errorf("parse: %w", err)
	}
	if err != nil {
		Use(id) // want "reason: unchecked-error"
	}
}

func UnmarkedLookup(provider Provider) (Record, bool, error) {
	value, ok, err := provider.Lookup()
	if err != nil {
		err = fmt.Errorf("lookup: %w", err)
	}
	return value, ok, err // want "reason: unchecked-presence"
}

func MissingPresence(provider Provider) {
	value, _, err := provider.Lookup()
	if err != nil {
		err = fmt.Errorf("lookup: %w", err)
	}
	if err == nil {
		UseRecord(value) // want "reason: unchecked-presence"
	}
}

func wrapError(err error) error { return fmt.Errorf("wrapped: %w", err) }

func maybeSwallow(err error, swallow bool) error {
	if swallow {
		return nil
	}
	return err
}
