package decodeflow

import (
	"database/sql"
	"encoding"
	"encoding/json"
	"io"

	"encodingvalues"
)

func consume(encodingvalues.ID) {}

func JSON(data []byte, reader io.Reader) {
	var value encodingvalues.ID
	if err := json.Unmarshal(data, &value); err != nil {
		return
	}
	consume(value)
	var decoded encodingvalues.ID
	if err := json.NewDecoder(reader).Decode(&decoded); err == nil {
		consume(decoded)
	}
}

func SQL(row *sql.Row) {
	var id encodingvalues.ID
	var raw string
	if err := row.Scan(&id, &raw); err != nil {
		return
	}
	consume(id)
	consume(encodingvalues.ID(raw)) // want "may be an empty encodingvalues.ID"
}

func Hooks(data []byte, raw any) {
	var id encodingvalues.ID
	if err := id.UnmarshalText(data); err != nil { return }
	consume(id)
	var other encodingvalues.ID
	if err := other.Scan(raw); err != nil { return }
	consume(other)
}

func Declared(decoder encodingvalues.Decoder) {
	var value encodingvalues.ID
	if err := decoder.Decode(&value); err != nil { return }
	consume(value)
}

func Unchecked(data []byte) {
	var value encodingvalues.ID
	json.Unmarshal(data, &value)
	consume(value) // want "may be an empty encodingvalues.ID"
}

func Failure(data []byte) {
	var value encodingvalues.ID
	if err := json.Unmarshal(data, &value); err != nil {
		consume(value) // want "may be an empty encodingvalues.ID"
	}
}

func MissingField(data []byte) {
	var envelope struct{ ID encodingvalues.ID }
	if err := json.Unmarshal(data, &envelope); err != nil { return }
	consume(envelope.ID) // want "may be an empty encodingvalues.ID"
}

func EncodeStillConsumes() {
	var value encodingvalues.ID
	json.Marshal(value) // want "argument is an empty encodingvalues.ID"
	encodingvalues.RoundTrip(&value) // want "argument is an empty encodingvalues.ID"
}

func RootContract(data []byte) {
	var object encodingvalues.Object
	if err := json.Unmarshal(data, &object); err != nil { return }
	consume(object.ID())
	var array encodingvalues.ArrayID
	json.Marshal(array) // want "argument is a zero encodingvalues.ArrayID"
	if err := json.Unmarshal(data, &array); err != nil { return }
	json.Marshal(array)
}

func ReplacedDestination(row *sql.Row, other *encodingvalues.ID) {
	var id encodingvalues.ID
	outputs := []any{&id}
	outputs[0] = other
	if err := row.Scan(outputs...); err != nil { return }
	consume(id) // want "argument is an empty encodingvalues.ID"
}

func FailedOverwrite(data []byte) {
	id, err := encodingvalues.NewID("valid")
	if err != nil { return }
	if err := json.Unmarshal(data, &id); err != nil {
		consume(id) // want "may be an empty encodingvalues.ID"
	}
}

func UncheckedOverwrite(data []byte) {
	id, err := encodingvalues.NewID("valid")
	if err != nil { return }
	json.Unmarshal(data, &id)
	consume(id) // want "may be an empty encodingvalues.ID"
}

func InterfaceDecoders(data []byte, raw any) {
	var jsonID encodingvalues.ID
	var jsonDecoder json.Unmarshaler = &jsonID
	if err := jsonDecoder.UnmarshalJSON(data); err != nil { return }
	consume(jsonID)
	var textID encodingvalues.ID
	var textDecoder encoding.TextUnmarshaler = &textID
	if err := textDecoder.UnmarshalText(data); err != nil { return }
	consume(textID)
	var sqlID encodingvalues.ID
	var scanner sql.Scanner = &sqlID
	if err := scanner.Scan(raw); err != nil { return }
	consume(sqlID)
}

func FailedArrayOverwrite(data []byte) {
	id, err := encodingvalues.NewID("valid")
	if err != nil { return }
	values := [1]encodingvalues.ID{id}
	if err := json.Unmarshal(data, &values); err != nil {
		json.Marshal(values) // want "may.*empty encodingvalues.ID"
	}
}

func PointerFactory(data []byte) {
	id, err := encodingvalues.NewID("valid")
	if err != nil { return }
	object := encodingvalues.NewObjectPointer(id)
	if err := json.Unmarshal(data, object); err != nil {
		object.Snapshot() // want "may be a zero encodingvalues.Object"
		return
	}
	object.Snapshot()
}

func PointerParameter(data []byte, object *encodingvalues.Object) {
	if err := json.Unmarshal(data, object); err != nil {
		object.Snapshot() // want "may be a zero encodingvalues.Object"
		return
	}
	object.Snapshot()
}
