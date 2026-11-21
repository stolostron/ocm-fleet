package serializationpolicy

import "database/sql/driver"

//domain:constructors NewMissing
//domain:serialization json
type Missing string // want "json serialization requires.*UnmarshalJSON"

func NewMissing(raw string) Missing { return Missing(raw) }

//domain:constructors NewTextOnly
//domain:serialization text
type TextOnly string // want "text serialization requires"

func NewTextOnly(raw string) TextOnly { return TextOnly(raw) }

//domain:boundary json encode=missing
func WrongParameter(value any) error { return nil } // want "unknown boundary parameter missing"

//domain:boundary json encode=_
func BlankEncodeParameter(_ any, _ any) error { return nil } // want "unknown boundary parameter _"

//domain:boundary json decode=_
func BlankDecodeParameter(_ any, _ any) error { return nil } // want "unknown boundary parameter _"

//domain:boundary json decode=value
func NoCheckedSuccess(value any) {} // want "decode boundary requires exactly one error result"

//domain:boundary json types=Missing
func WrongType[T any](value T) {} // want "unknown boundary type parameter Missing"

//domain:boundary json results=2
func WrongResult() error { return nil } // want "boundary result index 2 is out of range"

//domain:boundary binary encode=value
func UnknownCodec(value any) {} // want "unknown serialization codec binary"

//domain:constructors NewVariadic
//domain:serialization json
type Variadic string // want "json serialization requires.*UnmarshalJSON"

func NewVariadic(s string) Variadic           { return Variadic(s) }
func (*Variadic) UnmarshalJSON(...byte) error { return nil }

type CodecError struct{}

func (*CodecError) Error() string { return "codec error" }

//domain:constructors NewWrongError
//domain:serialization json
type WrongError string // want "json serialization requires.*UnmarshalJSON"

func NewWrongError(s string) WrongError              { return WrongError(s) }
func (*WrongError) UnmarshalJSON([]byte) *CodecError { return nil }

type ScanInput interface{}

//domain:constructors NewWrongInput
//domain:serialization sql
type WrongInput string // want "sql serialization requires.*Scan"

func NewWrongInput(s string) WrongInput         { return WrongInput(s) }
func (*WrongInput) Scan(ScanInput) error        { return nil }
func (WrongInput) Value() (driver.Value, error) { return nil, nil }

type Bytes = []byte
type Error = error

//domain:constructors NewAliasCodec
//domain:serialization json
type AliasCodec struct{}

func NewAliasCodec() AliasCodec                { return AliasCodec{} }
func (*AliasCodec) UnmarshalJSON(Bytes) Error  { return nil }
func (AliasCodec) MarshalJSON() (Bytes, Error) { return nil, nil }

//domain:constructors NewPointerValue
//domain:serialization sql
type PointerValue struct{} // want "sql serialization requires.*Value"

func NewPointerValue() PointerValue                { return PointerValue{} }
func (*PointerValue) Scan(any) Error               { return nil }
func (PointerValue) Value() (*driver.Value, Error) { return nil, nil }

//domain:constructors NewTextFallback
//domain:serialization json
type TextFallback struct{ raw string }

func NewTextFallback(raw string) TextFallback           { return TextFallback{raw: raw} }
func (*TextFallback) UnmarshalJSON([]byte) error         { return nil }
func (value TextFallback) MarshalJSON() string           { return value.raw }
func (value TextFallback) MarshalText() ([]byte, error) { return []byte(value.raw), nil }

//domain:boundary json decode=destination,
func (*PointerValue) EmptySelector(destination any) error { return nil } // want "nonempty comma-separated list"

//domain:boundary json decode=destination
func GenericDestination[V ~int](destination V) error { return nil } // want "must be a pointer or interface destination"
