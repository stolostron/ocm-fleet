package decodeflow

import (
	"encoding/json"
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

//domain:boundary json encode=value
func encodeDeclared(value ignoredFields) { consume(value.ID) }

func UseIgnoredFieldsOutsideJSON() ignoredFields {
	value := ignoredFields{}
	json.Marshal(value)
	encodeDeclared(value) // want "at path ID"
	return value          // want "at path ID"
}
