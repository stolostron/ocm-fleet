package badpolicy

//domain:constructors Missing
type MissingConstructor string // want "unknown constructor Missing"

//domain:constructors Value
type NotAFunction string // want "Value is not a function"

var Value = 1

//domain:constructors NewAlias
type Alias = string // want "domain:constructors requires a defined type"

func NewAlias(raw string) Alias { return raw }

//domain:constructors NewList
type List []string // want "requires a scalar, array, or struct type"

func NewList() List { return nil }

//domain:constructors
type NoConstructors string // want "list at least one constructor"

//domain:zero-valid
type Unprotected string // want "domain:zero-valid requires domain:constructors"

//domain:constructors NewTypo
//domain:zerovalid
type Typo string // want "unknown domain directive domain:zerovalid"

func NewTypo(raw string) Typo { return Typo(raw) }

//domain:constructors NewGeneric
type Generic[T any] struct{ value T } // want "does not support generic type declarations"

func NewGeneric[T any](value T) Generic[T] { return Generic[T]{value: value} }

//domain:constructors NewGrouped
type ( // want "put domain directives on each type in a grouped declaration"
	Grouped string
	Other   string
)

func NewGrouped(raw string) Grouped { return Grouped(raw) }
