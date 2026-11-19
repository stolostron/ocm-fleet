package decodeflow

import (
	"database/sql"
	"encoding/json"

	"encodingvalues"
)

func ScanWithAliasingAlternative(row *sql.Row, alias bool) (encodingvalues.ID, error) {
	var id encodingvalues.ID
	var other string
	var output any = &other
	if alias {
		output = (*string)(&id) // want "conversion uses an empty encodingvalues.ID"
	}
	if err := row.Scan(&id, output); err != nil {
		return "", err
	}
	return id, nil // want "return may produce an empty encodingvalues.ID"
}

func ScanWithDisjointAlternatives(row *sql.Row, choose bool) (encodingvalues.ID, error) {
	var id encodingvalues.ID
	var first, second string
	var output any = &first
	if choose {
		output = &second
	}
	if err := row.Scan(&id, output); err != nil {
		return "", err
	}
	return id, nil
}

func DecodeBorrowedSlot(first, second **encodingvalues.ID, data []byte) (encodingvalues.ID, error) {
	var a, b encodingvalues.ID
	*first = &a  // want "assignment stores an empty encodingvalues.ID"
	*second = &b // want "assignment stores an empty encodingvalues.ID"
	if err := json.Unmarshal(data, *first); err != nil {
		return "", err
	}
	return a, nil // want "return may produce an empty encodingvalues.ID"
}

func DecodePrivateSlots(data []byte) (encodingvalues.ID, error) {
	var a, b encodingvalues.ID
	first, second := new(*encodingvalues.ID), new(*encodingvalues.ID)
	*first = &a
	*second = &b
	if err := json.Unmarshal(data, *first); err != nil {
		return "", err
	}
	return a, nil
}

func DecodeAfterHandoff(data []byte) (encodingvalues.ID, error) {
	var a, b encodingvalues.ID
	slots := []*encodingvalues.ID{&a}
	done := make(chan struct{})
	go func(target []*encodingvalues.ID, next *encodingvalues.ID) {
		target[0] = next
		close(done)
	}(slots, &b) // want "argument contains an empty encodingvalues.ID" "argument is an empty encodingvalues.ID"
	<-done
	if err := json.Unmarshal(data, slots[0]); err != nil {
		return "", err
	}
	return a, nil // want "return may produce an empty encodingvalues.ID"
}
