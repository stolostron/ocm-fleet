package serialization

import (
	"database/sql"
	"encoding/json"
	"strconv"

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

func DecodeDefinedPointer(data []byte, id encodingvalues.ID, decoder *json.Decoder) {
	type plainPointer *string
	var plain string
	json.Unmarshal(data, plainPointer(&plain))
	json.Unmarshal(data, &id)
	json.Unmarshal(data, encodingvalues.IDPointerAlias(&id))
	json.Unmarshal(data, encodingvalues.IDPointer(&id)) // want "default JSON decoding can bypass encodingvalues.ID validation through a defined pointer"
	boxed := any(encodingvalues.IDPointer(&id))
	decoder.Decode(boxed) // want "default JSON decoding can bypass encodingvalues.ID validation through a defined pointer"
	json.Marshal(encodingvalues.IDPointer(&id))
}

func EncodeBoxedArrayElement(bad encodingvalues.Unsupported) {
	json.Marshal(([1]any{bad})[0]) // want "does not declare json serialization"
	json.Marshal(([2]any{bad, "plain"})[1])
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

type textRestore struct {
	Hidden encodingvalues.Unsupported `json:"-"`
}

func (value *textRestore) UnmarshalText(data []byte) error {
	value.Hidden = encodingvalues.NewUnsupported(string(data))
	return nil
}

type boxedTextRestore struct {
	Hidden any `json:"-"`
}

func (value *boxedTextRestore) UnmarshalText(data []byte) error {
	if _, ok := value.Hidden.(encodingvalues.Unsupported); ok {
		value.Hidden = encodingvalues.NewUnsupported(string(data))
	}
	return nil
}

func DecodeThroughTextHooks(bad encodingvalues.Unsupported, decoder *json.Decoder) {
	var typed textRestore
	json.Unmarshal([]byte(`"restored"`), &typed) // want "Hidden.*does not declare json serialization"
	boxed := boxedTextRestore{Hidden: bad}
	decoder.Decode(&boxed) // want "Hidden.*does not declare json serialization"
	var plain customEnvelope
	json.Unmarshal([]byte(`{}`), &plain)
}

//domain:constructors newJSONFirstKey
//domain:serialization text
type jsonFirstKey int

func newJSONFirstKey(n int) jsonFirstKey { return jsonFirstKey(n) }

func (key jsonFirstKey) MarshalText() ([]byte, error) { return []byte(strconv.Itoa(int(key))), nil }

func (key *jsonFirstKey) UnmarshalText(data []byte) error {
	n, err := strconv.Atoi(string(data))
	if err != nil {
		return err
	}
	*key = newJSONFirstKey(n)
	return nil
}

func (*jsonFirstKey) UnmarshalJSON([]byte) error { return nil }

//domain:constructors newTextMapKey
//domain:serialization text
type textMapKey int

func newTextMapKey(n int) textMapKey { return textMapKey(n) }

func (key textMapKey) MarshalText() ([]byte, error) { return []byte(strconv.Itoa(int(key))), nil }

func (key *textMapKey) UnmarshalText(data []byte) error {
	n, err := strconv.Atoi(string(data))
	if err != nil {
		return err
	}
	*key = newTextMapKey(n)
	return nil
}

func DecodeJSONMapKeys() {
	data := []byte(`{"1":true}`)
	var jsonFirst map[jsonFirstKey]bool
	json.Unmarshal(data, &jsonFirst) // want "at path \\[key\\]: .*does not declare json serialization"
	json.Marshal(jsonFirst)
	var textOnly map[textMapKey]bool
	json.Unmarshal(data, &textOnly)
}

func EncodeBoxedFieldSelector(bad encodingvalues.Unsupported) {
	json.Marshal((struct{ Payload any }{Payload: bad}).Payload) // want "does not declare json serialization"
	json.Marshal((struct{ Payload any }{Payload: "ok"}).Payload)
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
