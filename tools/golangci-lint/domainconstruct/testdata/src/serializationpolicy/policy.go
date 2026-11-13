package serializationpolicy

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

//domain:boundary json decode=value
func NoCheckedSuccess(value any) {} // want "decode boundary requires exactly one error result"

//domain:boundary json types=Missing
func WrongType[T any](value T) {} // want "unknown boundary type parameter Missing"

//domain:boundary json results=2
func WrongResult() error { return nil } // want "boundary result index 2 is out of range"

//domain:boundary binary encode=value
func UnknownCodec(value any) {} // want "unknown serialization codec binary"
