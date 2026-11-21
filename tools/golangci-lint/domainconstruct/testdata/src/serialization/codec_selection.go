package serialization

import (
	"database/sql"
	"encoding/json"

	"encodingvalues"
)

var initializerBytes, initializerError = json.Marshal(&struct{ Payload any }{Payload: encodingvalues.NewUnsupported("fixture")}) // want "Payload.*does not declare json serialization"

var closureBytes, closureError = func() ([]byte, error) {
	return json.Marshal(&struct{ Payload any }{Payload: encodingvalues.NewUnsupported("fixture")}) // want "Payload.*does not declare json serialization"
}()

func DecodeProtectedByteSlice(data []byte, decoder *json.Decoder) {
	var levels []encodingvalues.ByteLevel
	json.Unmarshal(data, &levels) // want "default JSON decoding.*encodingvalues.ByteLevel"
	decoder.Decode(&levels)       // want "default JSON decoding.*encodingvalues.ByteLevel"
	var boxed any = &levels
	json.Unmarshal(data, boxed) // want "default JSON decoding.*encodingvalues.ByteLevel"
	var nested struct{ Levels []encodingvalues.ByteLevel }
	json.Unmarshal(data, &nested) // want "Levels.*default JSON decoding.*encodingvalues.ByteLevel"
	var bytes []byte
	json.Unmarshal(data, &bytes)
	decoder.Decode(&bytes)
	var checked encodingvalues.ByteLevels
	json.Unmarshal(data, &checked)
	decoder.Decode(&checked)
}

type PromotedEncoder struct {
	encodingvalues.ID
	Other encodingvalues.Unsupported
}

func EncodeSelectedReceiver(value PromotedEncoder) {
	json.Marshal(value) // want "Other.*does not declare json serialization"
	value.MarshalJSON()
	PromotedEncoder.MarshalJSON(value)
	saved := PromotedEncoder.MarshalJSON
	saved(value)
}

func DecodeBoxedValue(value encodingvalues.Unsupported) {
	var replaced any = value
	json.Unmarshal(nil, &replaced)
	var nilPointer any = (*encodingvalues.Unsupported)(nil)
	json.Unmarshal([]byte("\"ok\""), &nilPointer)
	var followed any = &value
	json.Unmarshal(nil, &followed) // want "does not declare json serialization"
	json.Marshal(&replaced)        // want "does not declare json serialization"
}

func EncodeWithoutSQLValue(db *sql.DB, value encodingvalues.ScanOnly) {
	db.Exec("SELECT ?", value) // want "does not declare sql serialization"
}

type pointerHook struct {
	Value encodingvalues.Unsupported `json:"-"`
}

func (value *pointerHook) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(value.Value))
}

type boxedHook struct {
	Value any `json:"-"`
}

func (value *boxedHook) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.Value)
}

type embeddedHookField struct {
	Child pointerHook
}

func EncodeHookFields(value encodingvalues.Unsupported) {
	typed := pointerHook{Value: value}
	json.Marshal(&typed) // want "Value.*does not declare json serialization"
	json.Marshal(typed)
	json.Marshal([]pointerHook{typed}) // want "Value.*does not declare json serialization"
	json.Marshal(map[string]pointerHook{"key": typed})
	json.Marshal(struct{ *embeddedHookField }{&embeddedHookField{Child: typed}}) // want "Child.Value.*does not declare json serialization"
	json.Unmarshal(nil, &typed)

	boxed := boxedHook{Value: value}
	json.Marshal(&boxed) // want "Value.*does not declare json serialization"
	json.Marshal(boxed)
}

type customEnvelope struct {
	Hidden encodingvalues.Unsupported `json:"-"`
}

type customBox struct {
	Hidden any `json:"-"`
}

//domain:boundary json encode=value
func encodeHidden(value customEnvelope) ([]byte, error) {
	return json.Marshal(string(value.Hidden))
}

//domain:boundary json encode=value
func encodeHiddenBox(value customBox) ([]byte, error) {
	return json.Marshal(value.Hidden)
}

func EncodeHiddenFields(bad encodingvalues.Unsupported, encoder *json.Encoder) {
	typed := customEnvelope{Hidden: bad}
	boxed := customBox{Hidden: bad}
	encodeHidden(typed)    // want "Hidden.*does not declare json serialization"
	encodeHiddenBox(boxed) // want "Hidden.*does not declare json serialization"
	json.Marshal(typed)
	json.Marshal(boxed)
	encoder.Encode(typed)
	encoder.Encode(boxed)
	json.Unmarshal(nil, &typed)
}

type jsonRestore struct {
	Hidden encodingvalues.Unsupported `json:"-"`
}

func (value *jsonRestore) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	value.Hidden = encodingvalues.NewUnsupported(raw)
	return nil
}

type boxedJSONRestore struct {
	Hidden any `json:"-"`
}

func (value *boxedJSONRestore) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := value.Hidden.(encodingvalues.Unsupported); ok {
		value.Hidden = encodingvalues.NewUnsupported(raw)
	}
	return nil
}

func DecodeHookFields(data []byte, bad encodingvalues.Unsupported, decoder *json.Decoder) {
	var typed jsonRestore
	json.Unmarshal(data, &typed) // want "Hidden.*does not declare json serialization"
	boxed := boxedJSONRestore{Hidden: bad}
	decoder.Decode(&boxed) // want "Hidden.*does not declare json serialization"
	plain := customBox{Hidden: bad}
	decoder.Decode(&plain)
}

type privateValue struct{ hidden encodingvalues.Unsupported }
type privateBox struct{ hidden any }

//domain:boundary text encode=value
func encodePrivateText(value privateValue) string { return string(value.hidden) }

//domain:boundary text encode=value
func encodePrivateTextBox(value privateBox) string {
	return string(value.hidden.(encodingvalues.Unsupported))
}

//domain:boundary sql encode=value
func encodePrivateSQL(value privateValue) string { return string(value.hidden) }

//domain:boundary sql encode=value
func encodePrivateSQLBox(value privateBox) string {
	return string(value.hidden.(encodingvalues.Unsupported))
}

func EncodePrivateFields(bad encodingvalues.Unsupported) {
	typed := privateValue{hidden: bad}
	boxed := privateBox{hidden: bad}
	encodePrivateText(typed)    // want "hidden.*does not declare text serialization"
	encodePrivateTextBox(boxed) // want "hidden.*does not declare text serialization"
	encodePrivateSQL(typed)     // want "hidden.*does not declare sql serialization"
	encodePrivateSQLBox(boxed)  // want "hidden.*does not declare sql serialization"
	json.Marshal(typed)
	json.Marshal(boxed)
}

func EncodeShiftedBoxedSlice(bad encodingvalues.Unsupported, index int) {
	values := []any{"valid", bad}
	json.Marshal(values[index:index][:1]) // want "does not declare json serialization"
	json.Marshal(values[:1])
	json.Marshal(values[index:index])
}
