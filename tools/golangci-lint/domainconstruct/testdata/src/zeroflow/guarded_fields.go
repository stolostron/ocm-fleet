package zeroflow

import "errors"

// These guards prove only nonzero state. The raw conversions still violate the
// construction convention, and the typed checks still belong at a boundary.
func GuardedPointerReturn(raw string) (*Request, error) {
	value := Request{ID: ID(raw)}
	if value.ID == "" { // want "zero check on required zeroflow.ID; validity should be established by construction or checked decoding"
		return nil, errors.New("empty ID")
	}
	return &value, nil
}

func GuardedValueReturn(raw string) (Request, error) {
	value := Request{ID: ID(raw)}
	if value.ID == "" { // want "rule: domainzero[.]zero-comparison"
		return Request{}, errors.New("empty ID")
	}
	return value, nil
}

func GuardedNestedReturn(raw string) (*struct{ Inner Request }, error) {
	value := struct{ Inner Request }{Inner: Request{ID: ID(raw)}}
	if value.Inner.ID == "" { // want "rule: domainzero[.]zero-comparison"
		return nil, errors.New("empty ID")
	}
	return &value, nil
}

func GuardedCopyAfterCheck(raw string) (Request, error) {
	value := Request{ID: ID(raw)}
	if value.ID == "" { // want "rule: domainzero[.]zero-comparison"
		return Request{}, errors.New("empty ID")
	}
	copy := value
	return copy, nil
}

func GuardedCopyBeforeCheck(raw string) (Request, error) {
	value := Request{ID: ID(raw)}
	copy := value
	if value.ID == "" { // want "rule: domainzero[.]zero-comparison"
		return Request{}, errors.New("empty ID")
	}
	return copy, nil
}

func GuardedCopiedField(raw string) (Request, error) {
	value := Request{ID: ID(raw)}
	copy := value
	if copy.ID == "" { // want "rule: domainzero[.]zero-comparison"
		return Request{}, errors.New("empty ID")
	}
	return copy, nil
}

func GuardedSiblingWrite(raw string) (*struct {
	ID   ID
	Note string
}, error) {
	value := struct {
		ID   ID
		Note string
	}{ID: ID(raw)}
	if value.ID == "" { // want "rule: domainzero[.]zero-comparison"
		return nil, errors.New("empty ID")
	}
	value.Note = "changed"
	return &value, nil
}

func GuardedFieldLeavesSiblingZero(raw string) (*struct{ First, Second ID }, error) {
	value := struct{ First, Second ID }{First: ID(raw)}
	if value.First == "" { // want "rule: domainzero[.]zero-comparison"
		return nil, errors.New("empty ID")
	}
	return &value, nil // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*path: \"Second\""
}

func GuardedFieldReset(raw string) (*Request, error) {
	value := Request{ID: ID(raw)}
	if value.ID == "" { // want "rule: domainzero[.]zero-comparison"
		return nil, errors.New("empty ID")
	}
	value.ID = ""
	return &value, nil // want "(?s)rule: domainzero[.]invalid-zero; certainty: known"
}

func GuardedWholeValueReset(raw string) (*Request, error) {
	value := Request{ID: ID(raw)}
	alias := &value
	if value.ID == "" { // want "rule: domainzero[.]zero-comparison"
		return nil, errors.New("empty ID")
	}
	*alias = Request{}
	return &value, nil // want "(?s)rule: domainzero[.]invalid-zero; certainty: known"
}

func GuardedConditionalReset(raw string, reset bool) (*Request, error) {
	value := Request{ID: ID(raw)}
	if value.ID == "" { // want "rule: domainzero[.]zero-comparison"
		return nil, errors.New("empty ID")
	}
	if reset {
		value.ID = ""
	}
	return &value, nil // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible"
}

func GuardedCopyPrecedesInitialization(valid ID) (Request, error) {
	var value Request
	copy := value
	value.ID = valid
	if value.ID == "" { // want "rule: domainzero[.]zero-comparison"
		return Request{}, errors.New("empty ID")
	}
	return copy, nil // want "(?s)rule: domainzero[.]invalid-zero; certainty: known"
}

func GuardedFieldHelperReset(raw string) (*Request, error) {
	value := Request{ID: ID(raw)}
	if value.ID == "" { // want "rule: domainzero[.]zero-comparison"
		return nil, errors.New("empty ID")
	}
	resetGuardedID(&value.ID)
	return &value, nil // want "(?s)rule: domainzero[.]invalid-zero; certainty: known"
}

func resetGuardedID(id *ID) {
	*id = "" // want "rule: domainzero[.]invalid-zero"
}

func GuardedBorrowedAliasWrite(raw string) (*Request, error) {
	value := Request{ID: ID(raw)}
	if value.ID == "" { // want "rule: domainzero[.]zero-comparison"
		return nil, errors.New("empty ID")
	}
	alias := borrowGuardedID(&value.ID)
	*alias = ""        // want "rule: domainzero[.]invalid-zero"
	return &value, nil // want "rule: domainzero[.]invalid-zero"
}

func borrowGuardedID(id *ID) *ID { return id }
