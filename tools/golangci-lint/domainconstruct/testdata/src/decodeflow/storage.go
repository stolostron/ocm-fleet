package decodeflow

import (
	"database/sql"
	"encoding/json"

	"encodingvalues"
)

func AppendingAtCapacityKeepsDecoderDestination(data []byte, more []*encodingvalues.ID) (encodingvalues.ID, error) {
	var id encodingvalues.ID
	slots := []*encodingvalues.ID{&id}
	_ = append(slots[:1:1], more...)
	if err := json.Unmarshal(data, slots[0]); err != nil {
		return "", err
	}
	return id, nil
}

func SuccessfulRawDecodeCanInvalidateElement(valid encodingvalues.ID) []encodingvalues.ID {
	values := []encodingvalues.ID{valid, valid}
	if err := json.Unmarshal([]byte(`""`), (*string)(&values[0])); err != nil {
		return nil
	}
	return values // want "return may contain an empty encodingvalues.ID"
}

func SuccessfulDecodeKeepsDisjointElement(valid encodingvalues.ID) []encodingvalues.ID {
	values := []encodingvalues.ID{valid, valid}
	if err := json.Unmarshal([]byte(`""`), (*string)(&values[0])); err != nil {
		return nil
	}
	return values[1:]
}

func SuccessfulNativeDecodeRestoresElement(data []byte, valid encodingvalues.ID) []encodingvalues.ID {
	values := []encodingvalues.ID{valid}
	if err := json.Unmarshal(data, &values[0]); err != nil {
		return nil
	}
	return values
}

//domain:constructors newWireID
//domain:zero-valid
//domain:serialization json
type wireID string

func newWireID(value string) wireID { return wireID(value) }

func (value *wireID) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, (*string)(value))
}

func DecodeThroughOtherPolicy() (encodingvalues.ID, error) {
	var id encodingvalues.ID
	if err := json.Unmarshal([]byte(`""`), (*wireID)(&id)); err != nil { // want "conversion uses an empty encodingvalues.ID"
		return "", err
	}
	return id, nil // want "return may produce an empty encodingvalues.ID"
}

func DecodeThroughOtherPolicyOverwritesValidElement(valid encodingvalues.ID) []encodingvalues.ID {
	values := []encodingvalues.ID{valid, valid}
	if err := json.Unmarshal([]byte(`""`), (*wireID)(&values[0])); err != nil {
		return nil
	}
	return values // want "return may contain an empty encodingvalues.ID"
}

func DecodeThroughOtherPolicyKeepsDisjointElement(valid encodingvalues.ID) []encodingvalues.ID {
	values := []encodingvalues.ID{valid, valid}
	if err := json.Unmarshal([]byte(`""`), (*wireID)(&values[0])); err != nil {
		return nil
	}
	return values[1:]
}

func DecodeSameBoxedDestination(data []byte, choose bool) (encodingvalues.ID, error) {
	var id encodingvalues.ID
	var destination any
	if choose {
		destination = any(&id)
	} else {
		destination = any(&id)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return "", err
	}
	return id, nil
}

func DecodeDifferentPointerTypes(data []byte, choose bool) (encodingvalues.ID, error) {
	var id encodingvalues.ID
	var destination any = &id
	if choose {
		destination = (*string)(&id) // want "conversion uses an empty encodingvalues.ID"
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return "", err
	}
	return id, nil // want "return may produce an empty encodingvalues.ID"
}

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

func replaceSlot(slots []*encodingvalues.ID, next *encodingvalues.ID) {
	slots[0] = next
}

func slotCount(slots []*encodingvalues.ID) int { return len(slots) }

func DecodeAfterReplacedSlot(data []byte, a, b encodingvalues.ID) (encodingvalues.ID, error) {
	slots := [1]*encodingvalues.ID{&a}
	replaceSlot(slots[:], &b)
	a = ""
	if err := json.Unmarshal(data, slots[0]); err != nil {
		return "", err
	}
	return a, nil // want "return may produce an empty encodingvalues.ID"
}

func DecodeAfterDisjointSlotWrite(data []byte, a, b encodingvalues.ID) (encodingvalues.ID, error) {
	slots := [2]*encodingvalues.ID{&a, &b}
	replaceSlot(slots[1:], &b)
	a = ""
	if err := json.Unmarshal(data, slots[0]); err != nil {
		return "", err
	}
	return a, nil
}

func DecodeAfterReadOnlySlotUse(data []byte, a encodingvalues.ID) (encodingvalues.ID, error) {
	slots := [1]*encodingvalues.ID{&a}
	_ = slotCount(slots[:])
	a = ""
	if err := json.Unmarshal(data, slots[0]); err != nil {
		return "", err
	}
	return a, nil
}

func DecodeAfterMergedSlotWrite(data []byte, otherSlot bool) (encodingvalues.ID, error) {
	var a, b encodingvalues.ID
	slots := []*encodingvalues.ID{&a, &b}
	slot := &slots[0]
	if otherSlot {
		slot = &slots[1]
	}
	*slot = &b
	if err := json.Unmarshal(data, slots[0]); err != nil {
		return "", err
	}
	return a, nil // want "return .*empty encodingvalues.ID"
}

func DecodeAfterDisjointMergedSlotWrite(data []byte, otherSlot bool) (encodingvalues.ID, error) {
	var a, b encodingvalues.ID
	slots := []*encodingvalues.ID{&a, &b, &b}
	slot := &slots[1]
	if otherSlot {
		slot = &slots[2]
	}
	*slot = &b
	if err := json.Unmarshal(data, slots[0]); err != nil {
		return "", err
	}
	return a, nil
}
