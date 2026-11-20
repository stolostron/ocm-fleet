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
