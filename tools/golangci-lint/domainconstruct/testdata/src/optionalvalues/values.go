package optionalvalues

import "github.com/stolostron/ocm-fleet/server/pkg/optional"

//domain:constructors NewID
type ID string

func NewID(raw string) ID {
	if raw == "" {
		panic("empty identifier")
	}
	return ID(raw)
}

//domain:constructors NewTotal
//domain:zero-valid
type Total int

func NewTotal(raw int) Total { return Total(raw) }

type Record struct{ ID ID }

type Name = optional.Optional[ID]

type Generic[T any] = optional.Optional[T]

func Use(ID)                                    {}
func UseOptional(optional.Optional[ID])         {}
func UseRecord(Record)                          {}
func UseRecordOptional(optional.Optional[Record]) {}
