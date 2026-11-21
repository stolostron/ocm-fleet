package decodeflow

import (
	"database/sql"
	"encoding/json"
	"errors"

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

func SuccessfulRawDecodeCanInvalidateDynamicElement(valid encodingvalues.ID, index int) []encodingvalues.ID {
	values := []encodingvalues.ID{valid, valid}
	if err := json.Unmarshal([]byte(`""`), (*string)(&values[index])); err != nil {
		return nil
	}
	return values[:1] // want "return may contain an empty encodingvalues.ID"
}

func DynamicNativeDecodeCannotProveInitialization(data []byte, index int) {
	values := make([]encodingvalues.ID, 2)
	if err := json.Unmarshal(data, &values[index]); err != nil {
		return
	}
	consume(values[0]) // want "argument .* an empty encodingvalues.ID"
}

type decodeRow struct {
	Other encodingvalues.ID
	ID    encodingvalues.ID
	Label string
}

func SuccessfulRawDecodeCanInvalidateElementField(valid encodingvalues.ID) []decodeRow {
	rows := []decodeRow{{Other: valid, ID: valid}, {Other: valid, ID: valid}}
	if err := json.Unmarshal([]byte(`""`), (*string)(&rows[1].ID)); err != nil {
		return nil
	}
	consume(rows[1].Other)
	return rows // want "return may contain an empty encodingvalues.ID at path \\[1\\][.]ID"
}

func SuccessfulDecodeKeepsUnrelatedElementField(valid encodingvalues.ID, data []byte) []decodeRow {
	rows := []decodeRow{{Other: valid, ID: valid}}
	if err := json.Unmarshal(data, &rows[0].Label); err != nil {
		return nil
	}
	if err := json.Unmarshal(data, &rows[0].ID); err != nil {
		return nil
	}
	return rows
}

func SuccessfulRawDecodeCanInvalidateNestedArray(valid encodingvalues.ID) [1][1]encodingvalues.ID {
	rows := [1][1]encodingvalues.ID{{valid}}
	if err := json.Unmarshal([]byte(`""`), (*string)(&rows[0][0])); err != nil {
		return [1][1]encodingvalues.ID{{valid}}
	}
	return rows // want "return may contain an empty encodingvalues.ID at path \\[0\\]\\[0\\]"
}

func SuccessfulRawDecodeKeepsDisjointNestedArray(valid encodingvalues.ID) [1][1]encodingvalues.ID {
	rows := [2][1]encodingvalues.ID{{valid}, {valid}}
	if err := json.Unmarshal([]byte(`""`), (*string)(&rows[1][0])); err != nil {
		return [1][1]encodingvalues.ID{{valid}}
	}
	return [1][1]encodingvalues.ID(rows[:1])
}

func ReadFieldAfterDynamicDecode(valid encodingvalues.ID, index int) {
	rows := []decodeRow{{Other: valid, ID: valid}, {Other: valid, ID: valid}}
	if err := json.Unmarshal([]byte(`""`), (*string)(&rows[index].ID)); err != nil { // want "conversion may use an empty encodingvalues.ID"
		return
	}
	consume(rows[0].ID) // want "argument may be an empty encodingvalues.ID"
	consume(rows[0].Other)
}

func EncodingKeepsIgnoringOmittedElementField(valid encodingvalues.ID) {
	rows := []struct {
		Hidden encodingvalues.ID `json:"-"`
		Other  encodingvalues.ID
	}{{Hidden: valid, Other: valid}}
	if err := json.Unmarshal([]byte(`""`), (*string)(&rows[0].Hidden)); err != nil {
		return
	}
	json.Marshal(rows)
}

func SuccessfulDecodeThroughPointerSlot(valid encodingvalues.ID) (encodingvalues.ID, error) {
	id := valid
	raw := (*string)(&id)
	if err := json.Unmarshal([]byte(`""`), &raw); err != nil {
		return "", err
	}
	return id, nil // want "return may produce an empty encodingvalues.ID"
}

func SuccessfulDecodeThroughDisjointPointerSlot(valid encodingvalues.ID) (encodingvalues.ID, error) {
	id, other := valid, valid
	raw := (*string)(&other)
	if err := json.Unmarshal([]byte(`""`), &raw); err != nil {
		return "", err
	}
	return id, nil
}

func NativePointerSlotDecodeCanLeavePointeeZero(data []byte) (encodingvalues.ID, error) {
	var id encodingvalues.ID
	pointer := &id
	if err := json.Unmarshal(data, &pointer); err != nil {
		return "", err
	}
	return id, nil // want "return .* an empty encodingvalues.ID"
}

type decoderSlot struct{ ID *encodingvalues.ID }

func DecodeCannotProveDynamicOrigin(data []byte, write, read int) (encodingvalues.ID, error) {
	var id, other encodingvalues.ID
	parents := [2]decoderSlot{{ID: &other}, {ID: &other}}
	parents[write] = decoderSlot{ID: &id}
	slots := [1]decoderSlot{parents[read]}
	if err := json.Unmarshal(data, slots[0].ID); err != nil {
		return "", err
	}
	return id, nil // want "return produces an empty encodingvalues.ID"
}

func DecodeProvesFixedOrigin(data []byte) (encodingvalues.ID, error) {
	var id, other encodingvalues.ID
	parents := [2]decoderSlot{{ID: &other}, {ID: &other}}
	parents[0] = decoderSlot{ID: &id}
	slots := [1]decoderSlot{parents[0]}
	if err := json.Unmarshal(data, slots[0].ID); err != nil {
		return "", err
	}
	return id, nil
}

func DecodeAfterCaptureChangesSlot(data []byte) (encodingvalues.ID, error) {
	var id encodingvalues.ID
	var slot *encodingvalues.ID
	start, done := make(chan struct{}), make(chan struct{})
	go func() {
		<-start
		if other, err := encodingvalues.NewID("replacement"); err == nil {
			slot = &other
		}
		close(done)
	}()
	slot = &id
	close(start)
	<-done
	if err := json.Unmarshal(data, slot); err != nil {
		return "", err
	}
	return id, nil // want "return produces an empty encodingvalues.ID"
}

func keepLocalSlot(**encodingvalues.ID) {}

func DecodeAfterPrivateSlotHandoff(data []byte) (encodingvalues.ID, error) {
	var id encodingvalues.ID
	var slot *encodingvalues.ID
	keepLocalSlot(&slot)
	slot = &id
	done := make(chan struct{})
	go close(done)
	<-done
	if err := json.Unmarshal(data, slot); err != nil {
		return "", err
	}
	return id, nil
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

func ShortJSONDecodeClearsArray(valid encodingvalues.ID) ([1]encodingvalues.ID, error) {
	values := [1]encodingvalues.ID{valid}
	if err := json.Unmarshal([]byte(`[]`), &values); err != nil {
		return [1]encodingvalues.ID{}, err
	}
	return values, nil // want "return may contain an empty encodingvalues.ID"
}

func ShortJSONDecodeClearsElement(valid encodingvalues.ID, decoder *json.Decoder) (encodingvalues.ID, error) {
	values := [1]encodingvalues.ID{valid}
	if err := decoder.Decode(&values); err != nil {
		return "", err
	}
	return values[0], nil // want "return may .* an empty encodingvalues.ID"
}

func RepairArrayAfterJSONDecode(valid encodingvalues.ID, data []byte) ([1]encodingvalues.ID, error) {
	values := [1]encodingvalues.ID{valid}
	if err := json.Unmarshal(data, &values); err != nil {
		return [1]encodingvalues.ID{}, err
	}
	values[0] = valid
	return values, nil
}

type checkedIDArray [1]encodingvalues.ID

func (values *checkedIDArray) UnmarshalJSON(data []byte) error {
	var decoded []encodingvalues.ID
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if len(decoded) != len(values) {
		return errors.New("expected one ID")
	}
	values[0] = decoded[0]
	return nil
}

func JSONDecodeWithArrayHook(valid encodingvalues.ID, data []byte) (checkedIDArray, error) {
	values := checkedIDArray{valid}
	if err := json.Unmarshal(data, &values); err != nil {
		return checkedIDArray{}, err
	}
	return values, nil
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

func DecodeAfterDisjointSlotWrite(data []byte, a, b, c encodingvalues.ID) (encodingvalues.ID, error) {
	slots := [2]*encodingvalues.ID{&a, &b}
	replaceSlot(slots[1:], &c)
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
	var a, b, c encodingvalues.ID
	slots := []*encodingvalues.ID{&a, &b, &b}
	slot := &slots[1]
	if otherSlot {
		slot = &slots[2]
	}
	*slot = &c
	if err := json.Unmarshal(data, slots[0]); err != nil {
		return "", err
	}
	return a, nil
}

func DecodeThroughBoxedPointer(valid encodingvalues.ID) []encodingvalues.ID {
	values := []encodingvalues.ID{valid}
	var out any = (*string)(&values[0])
	if err := json.Unmarshal([]byte(`""`), &out); err != nil {
		return nil
	}
	return values // want "return may contain an empty encodingvalues.ID"
}

func DecodeReplacesBoxedValue(valid encodingvalues.ID) []encodingvalues.ID {
	values := []encodingvalues.ID{valid}
	var out any = string(values[0])
	if err := json.Unmarshal([]byte(`""`), &out); err != nil {
		return nil
	}
	return values
}

func DecodeMayReplaceBoxedPointer() (encodingvalues.ID, error) {
	var value encodingvalues.ID
	var out any = &value
	if err := json.Unmarshal([]byte(`null`), &out); err != nil {
		return "", err
	}
	return value, nil // want "return .*empty encodingvalues.ID"
}
