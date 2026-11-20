package protection

func SendProtected(out chan<- Record, raw struct {
	number int
	array  [1]int
	items  []int
	keys   map[int]int
}, value Record) {
	out <- raw // want "construct protection.Record through NewRecord"
	out <- value
}

func AppendIntoOwnedSlice(record Record, rest []int) {
	_ = append(record.items[:0], 1)       // want "modify protection.Record through NewRecord"
	_ = append(record.items[:0], rest...) // want "modify protection.Record through NewRecord"
	_ = append(record.items[:1:1], 1)
	_ = append(record.items[:1:1], rest...)
	_ = append(record.items[:0])
	_ = append(record.items, []int{}...)
}

func ConvertOwnedSlice(record Record) {
	_ = (*[1]int)(record.items) // want "do not expose mutable representation of protection.Record"
	_ = (*[0]int)(record.items)
	_ = [1]int(record.items)
}

type hydrationChild struct{}

func (*hydrationChild) UnmarshalJSON([]byte) error {
	_ = HydrationParent{Number: 1} // want "construct protection.HydrationParent through NewHydrationParent"
	return nil
}

//domain:constructors NewHydrationParent
//domain:serialization json
type HydrationParent struct { // want HydrationParent:"requires constructors"
	hydrationChild
	Number int
}

func NewHydrationParent() HydrationParent { return HydrationParent{Number: 1} }

type originBuilder[T any] struct{}

func (originBuilder[T]) Build() FromPromotedConstructor {
	return FromPromotedConstructor{Number: 1}
}

type ConstructionHost struct{ originBuilder[int] }

//domain:constructors ConstructionHost.Build
type FromPromotedConstructor struct{ Number int } // want FromPromotedConstructor:"requires constructors"
