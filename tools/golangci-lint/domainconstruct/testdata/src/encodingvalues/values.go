package encodingvalues

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

//domain:constructors NewID
//domain:serialization json text sql
type ID string

func NewID(value string) (ID, error) {
	if value == "" {
		return "", errors.New("empty ID")
	}
	return ID(value), nil
}

func (id ID) MarshalJSON() ([]byte, error) { return json.Marshal(string(id)) }

func (id *ID) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	value, err := NewID(raw)
	if err != nil {
		return err
	}
	*id = value
	return nil
}

func (id ID) MarshalText() ([]byte, error) { return []byte(id), nil }

func (id *ID) UnmarshalText(data []byte) error {
	value, err := NewID(string(data))
	if err != nil {
		return err
	}
	*id = value
	return nil
}

func (id ID) Value() (driver.Value, error) { return string(id), nil }

func (id *ID) Scan(raw any) error {
	value, ok := raw.(string)
	if !ok {
		return errors.New("expected a string")
	}
	return id.UnmarshalText([]byte(value))
}

//domain:constructors NewUnsupported
type Unsupported string

func NewUnsupported(raw string) Unsupported { return Unsupported(raw) }

//domain:constructors NewZeroValid
//domain:zero-valid
type ZeroValid string

func NewZeroValid(raw string) ZeroValid { return ZeroValid(raw) }

// Implementing a hook alone does not establish a domain restoration contract.
func (value *Unsupported) UnmarshalJSON([]byte) error { return nil }
func (value Unsupported) MarshalJSON() ([]byte, error) { return json.Marshal(string(value)) }
func (value *Unsupported) UnmarshalText([]byte) error { return errors.New("fixture") }
func (value Unsupported) MarshalText() ([]byte, error) { return []byte(value), nil }
func (value *Unsupported) Scan(any) error { return errors.New("fixture") }
func (value Unsupported) Value() (driver.Value, error) { return string(value), nil }

//domain:constructors NewJSONOnly
//domain:serialization json
type JSONOnly string

func NewJSONOnly(raw string) JSONOnly { return JSONOnly(raw) }
func (value *JSONOnly) UnmarshalJSON([]byte) error { return errors.New("fixture") }

//domain:boundary json types=I,O
func Transfer[I, O any](input I) (O, error) {
	var output O
	return output, errors.New("fixture")
}

//domain:boundary json encode=payload
func Send(payload any) error { return nil }

//domain:boundary json encode=value decode=value
func RoundTrip(value any) error {
	data, err := json.Marshal(value)
	if err != nil { return err }
	return json.Unmarshal(data, value)
}

type Decoder interface {
	//domain:boundary json decode=destination
	Decode(destination any) error
}

type Execution[T any] interface {
	//domain:boundary json results=0
	Result() (T, error)
}

//domain:snapshot
type Snapshot struct {
	ID string
	Children []Snapshot
}

//domain:constructors NewTextID
//domain:serialization text
type TextID string

func NewTextID(value string) TextID { return TextID(value) }
func (id TextID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *TextID) UnmarshalText(data []byte) error { return errors.New("fixture") }

//domain:constructors NewObject
//domain:serialization json
type Object struct { id ID }

func NewObject(id ID) Object { return Object{id: id} }
func NewObjectPointer(id ID) *Object { object := NewObject(id); return &object }
func (object *Object) Snapshot() string { return string(object.id) }
func (object Object) ID() ID { return object.id }
func (object Object) MarshalJSON() ([]byte, error) { return json.Marshal(string(object.id)) }
func (object *Object) UnmarshalJSON(data []byte) error {
	var id ID
	if err := json.Unmarshal(data, &id); err != nil { return err }
	*object = NewObject(id)
	return nil
}

//domain:constructors NewArrayID
//domain:serialization json
type ArrayID [16]byte

func NewArrayID() ArrayID { return ArrayID{1} }
func (id ArrayID) MarshalJSON() ([]byte, error) { return json.Marshal([16]byte(id)) }
func (id *ArrayID) UnmarshalJSON([]byte) error { *id = NewArrayID(); return nil }
