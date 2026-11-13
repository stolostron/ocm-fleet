package serialization

import (
	"database/sql"
	"database/sql/driver"
	"encoding"
	"encoding/json"
	"io"
	"time"

	"encodingvalues"
)

type Envelope struct {
	ID encodingvalues.ID
	Children []Envelope
	Optional *encodingvalues.ID
}

type UnsafeEnvelope struct {
	Items map[string][]encodingvalues.Unsupported
}

func Boundaries(id encodingvalues.ID, bad encodingvalues.Unsupported, zero encodingvalues.ZeroValid, reader io.Reader, writer io.Writer, row *sql.Row, db *sql.DB) {
	json.Marshal(id)
	json.Marshal(Envelope{ID: id})
	json.Marshal(map[string]encodingvalues.ID{"id": id})
	json.Marshal(encodingvalues.Snapshot{})
	json.Marshal(bad) // want "does not declare json serialization"
	json.MarshalIndent(UnsafeEnvelope{}, "", "  ") // want "Items.*does not declare json serialization"
	json.Marshal(zero) // want "does not declare json serialization"
	json.NewEncoder(writer).Encode(any(bad)) // want "does not declare json serialization"
	json.Unmarshal(nil, &bad) // want "does not declare json serialization"
	json.NewDecoder(reader).Decode(&bad) // want "does not declare json serialization"
	json.Unmarshal(nil, &id)
	row.Scan(&id)
	row.Scan(&bad) // want "does not declare sql serialization"
	db.Exec("insert into ids values (?)", id)
	db.Exec("insert into ids values (?)", bad) // want "does not declare sql serialization"
	encodingvalues.Transfer[Envelope, Envelope](Envelope{ID: id})
	encodingvalues.Transfer[Envelope, UnsafeEnvelope](Envelope{ID: id}) // want "Items.*does not declare json serialization"
	encodingvalues.Send(UnsafeEnvelope{}) // want "Items.*does not declare json serialization"
}

func ImportedInterfaces(decoder encodingvalues.Decoder, execution encodingvalues.Execution[UnsafeEnvelope], bad *encodingvalues.Unsupported) {
	decoder.Decode(bad) // want "does not declare json serialization"
	execution.Result() // want "Items.*does not declare json serialization"
}

//domain:snapshot
type Raw struct {
	ID string
	Time time.Time
	Optional *string
	Nested []map[string]encodingvalues.Snapshot
}

//domain:snapshot
type InvalidSnapshot struct { // want "snapshot.*ID.*domain value"
	Nested []Envelope
}

func JSONMapKeys(value map[encodingvalues.JSONOnly]string) {
	json.Marshal(value) // want "does not declare text serialization"
}

func DifferentMapCodecs(value map[encodingvalues.TextID]encodingvalues.TextID) {
	json.Marshal(value) // want "does not declare json serialization"
}

func BoxedValues(bad encodingvalues.Unsupported, id encodingvalues.ID, db *sql.DB) {
	var payload any = bad
	json.Marshal(payload) // want "does not declare json serialization"
	payload = id
	json.Marshal(payload)
	arguments := []any{bad}
	db.Exec("insert into ids values (?)", arguments...) // want "does not declare sql serialization"
	json.Marshal(struct{ Payload any }{Payload: bad}) // want "does not declare json serialization"
	pointer := &struct{ Payload any }{Payload: bad}
	json.Marshal(pointer) // want "does not declare json serialization"
	marshal := json.Marshal
	marshal(bad) // want "does not declare json serialization"
	transfer := encodingvalues.Transfer[encodingvalues.ID, encodingvalues.Unsupported]
	transfer(id) // want "does not declare json serialization"
}

//domain:snapshot
type HiddenDomainField struct { // want "snapshot.*Ignored.*domain value"
	Ignored encodingvalues.ID `json:"-"`
}

type UnrelatedScanner struct{}

func (UnrelatedScanner) Scan(any) error { return nil }

func UnrelatedMethod(bad encodingvalues.Unsupported) {
	UnrelatedScanner{}.Scan(bad)
}

func InterfaceHooks(bad encodingvalues.Unsupported, data []byte, raw any) {
	var decoder json.Unmarshaler = &bad
	decoder.UnmarshalJSON(data) // want "does not declare json serialization"
	var encoder json.Marshaler = bad
	encoder.MarshalJSON() // want "does not declare json serialization"
	var textDecoder encoding.TextUnmarshaler = &bad
	textDecoder.UnmarshalText(data) // want "does not declare text serialization"
	var textEncoder encoding.TextMarshaler = bad
	textEncoder.MarshalText() // want "does not declare text serialization"
	var scanner sql.Scanner = &bad
	scanner.Scan(raw) // want "does not declare sql serialization"
	var valuer driver.Valuer = bad
	valuer.Value() // want "does not declare sql serialization"
}
