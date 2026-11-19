package decodeflow

import (
	"encoding/json"
	"errors"
	"io"

	"encodingvalues"
)

type ignoredFields struct {
	ID encodingvalues.ID `json:"-"`
}

func EncodeIgnoresTaggedFields(writer io.Writer) {
	json.Marshal(ignoredFields{})
	json.MarshalIndent(&ignoredFields{}, "", "  ")
	json.NewEncoder(writer).Encode(make([]ignoredFields, 1))
	json.Marshal(struct{ Nested [1]ignoredFields }{})
	json.Marshal(struct{ Payload any }{Payload: ignoredFields{}})
}

func EncodeMethodForms(encoder *json.Encoder) {
	encode := encoder.Encode
	encode(ignoredFields{})
	(*json.Encoder).Encode(encoder, ignoredFields{})
}

func EncodeStillChecksVisibleFields() {
	json.Marshal(struct { // want "at path DashKey"
		Ignored encodingvalues.ID `json:"-"`
		DashKey encodingvalues.ID `json:"-,"`
	}{})
	json.Marshal(struct { // want "at path ID"
		ID encodingvalues.ID `json:",omitempty"`
	}{})
}

func EncodeBranch(flag bool) {
	var payload any = ignoredFields{}
	if flag {
		payload = &ignoredFields{}
	}
	json.Marshal(payload)
}

func EncodeFilledCollection(id encodingvalues.ID) {
	type item struct {
		Ignored encodingvalues.ID `json:"-"`
		ID      encodingvalues.ID
	}
	items := make([]item, 2)
	for i := range items {
		items[i] = item{ID: id}
	}
	json.Marshal(items)
}

func EncodeUncheckedResult(load func() (ignoredFields, error)) {
	payload, _ := load()
	json.Marshal(payload)
}

type jsonHook struct {
	Nested ignoredFields
}

func (value jsonHook) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(value.Nested.ID))
}

type textHook ignoredFields

func (value *textHook) MarshalText() ([]byte, error) {
	return []byte(value.ID), nil
}

func EncodeCustomHooks() {
	json.Marshal(jsonHook{})                 // want "at path Nested.ID"
	json.Marshal(&struct{ Hook textHook }{}) // want "at path Hook.ID"
}

type pointerHookOnly struct {
	Hidden encodingvalues.ID `json:"-"`
}

func (*pointerHookOnly) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

func EncodeNativeFieldSelection() {
	json.Marshal(struct{ hidden encodingvalues.ID }{})
	json.Marshal(pointerHookOnly{})
	json.Marshal(&pointerHookOnly{}) // want "at path Hidden"
	json.Marshal(make([]pointerHookOnly, 1)) // want "at path \\[0\\][.]Hidden"
	type embedded struct{ ID encodingvalues.ID }
	json.Marshal(struct{ embedded }{}) // want "at path embedded.ID"
	json.Marshal(struct {
		embedded
		ID string
	}{})
	type second struct{ ID encodingvalues.ID }
	json.Marshal(struct{ embedded; second }{})
}

func successOnlyBox(ready bool, id encodingvalues.ID) (any, error) {
	if ready {
		return id, nil
	}
	return nil, errors.New("missing")
}

func overwrittenBox() (any, error) {
	var box any = encodingvalues.ID("")
	box = 1
	return box, errors.New("missing")
}

func convertedBox(id encodingvalues.ID) (any, error) {
	return any(string(id)), errors.New("missing")
}

func nilPointerBox() (any, error) {
	return (*encodingvalues.ID)(nil), errors.New("missing")
}

func nonnilPointerBox() (any, error) {
	return new(encodingvalues.ID), errors.New("missing")
}

func UsePointerBoxes(use func(any)) {
	value, _ := nilPointerBox()
	use(value)
	value, _ = nonnilPointerBox()
	use(value) // want "argument may be an empty .*ID"
}

func deferredFailureBox(id encodingvalues.ID) (value any, err error) {
	defer func() { err = errors.New("missing") }()
	return id, nil
}

func skippedBoxReplacement(skip bool) (any, error) {
	var value any = encodingvalues.ID("")
	if skip {
		goto done
	}
	value = 1
done:
	return value, errors.New("missing")
}

func simultaneousBoxReplacement() (any, error) {
	var value any = encodingvalues.ID("")
	var result any
	value, result = 1, value
	return result, errors.New("missing")
}

func UseFailurePayloads(ready bool, id encodingvalues.ID) {
	value, _ := successOnlyBox(ready, id)
	json.Marshal(value)
	value, _ = overwrittenBox()
	json.Marshal(value)
	value, _ = convertedBox(id)
	json.Marshal(value)
	value, _ = deferredFailureBox(id)
	json.Marshal(value) // want "may be an empty .*ID"
	value, _ = skippedBoxReplacement(ready)
	json.Marshal(value) // want "may be an empty .*ID"
	value, _ = simultaneousBoxReplacement()
	json.Marshal(value) // want "may be an empty .*ID"
}

//domain:boundary json encode=value
func encodeDeclared(value ignoredFields) { consume(value.ID) }

func UseIgnoredFieldsOutsideJSON() ignoredFields {
	value := ignoredFields{}
	json.Marshal(value)
	encodeDeclared(value) // want "at path ID"
	return value          // want "at path ID"
}
