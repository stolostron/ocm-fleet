package protection

//domain:constructors NewRecord
type Record struct { // want Record:"requires constructors"
	number int
	array  [1]int
	items  []int
	keys   map[int]int
}

func NewRecord() Record { return Record{number: 1} }

type Embedded struct{ Record }
type Reference *Record

func Writes(embedded Embedded, ref Reference, record Record) {
	embedded.number = 0              // want "modify protection.Record through NewRecord"
	ref.number = 0                   // want "modify protection.Record through NewRecord"
	_ = record.array[:]              // want "do not expose mutable contents of protection.Record"
	clear(record.items)              // want "modify protection.Record through NewRecord"
	copy(record.items[1:], []int{2}) // want "modify protection.Record through NewRecord"
	delete(record.keys, 0)           // want "modify protection.Record through NewRecord"
	_ = len(record.items)

	shadow := struct {
		Record
		number int
	}{Record: record}
	shadow.number = 0
}

//domain:constructors NewID
type ID [2]byte // want ID:"requires constructors"

func NewID(raw [2]byte) ID { return ID(raw) }
func consume(ID)           {}

func UnnamedTransfers() ID {
	var id ID = [2]byte{1, 2} // want "construct protection.ID through NewID"
	id = [2]byte{3, 4}        // want "construct protection.ID through NewID"
	var zero ID = [2]byte{}
	id = zero
	consume(id)
	consume([2]byte{5, 6})                 // want "construct protection.ID through NewID"
	_ = struct{ ID ID }{ID: [2]byte{7, 8}} // want "construct protection.ID through NewID"
	return [2]byte{9, 10}                  // want "construct protection.ID through NewID"
}

func rawAppendPair() ([]ID, [2]byte) { return nil, [2]byte{1} }

func AppendTransfers(raw [2]byte, valid ID, values []ID) {
	_ = append(values, raw)     // want "construct protection.ID through NewID"
	_ = append(rawAppendPair()) // want "construct protection.ID through NewID"
	_ = append(values, valid, [2]byte{})
	_ = append(values, values...)
	_ = append([]byte(nil), "text"...)
}

func ReturnOwnedView(value Record) []int {
	return value.items[1:] // want "do not expose mutable contents of protection.Record"
}

func ReturnOwnedSlice(value Record) []int {
	return value.items // want "do not expose mutable contents of protection.Record"
}

func InspectOwnedSlice(value Record, independent []int) []int {
	_ = len(value.items[:])
	copy(independent, value.items[1:])
	return value.items[:0:0][:0]
}

//domain:constructors NewCount
type Count int // want Count:"requires constructors"

func NewCount(raw int) Count { return Count(raw) }

func Observations(count Count, record Record, id *ID) {
	_ = any(id)
	_ = count == (3 + 4)
	_ = count == (Count(1) + 2) // want "construct protection.Count through NewCount"
	switch count {
	case (3):
	}
	for _, record.number = range []int{1} { // want "modify protection.Record through NewRecord"
		consume(ID([2]byte{1})) // want "construct protection.ID through NewID"
	}
}

func recast[T ~int, U ~int](value U) T { return T(value) }
func genericZero[T ~int]() T           { return T(0) }

func Conversions(count Count, raw int) {
	_ = recast[Count](count)
	_ = genericZero[Count]()
	_ = recast[Count](raw) // want "construct protection.Count through NewCount"
}

func countPair(count Count) (bool, Count)         { return true, count }
func copyCount[T ~int, U ~int](_ bool, value U) T { return T(value) }

func TupleIdentity(count Count, raw int) {
	_ = copyCount[Count](countPair(count))
	_ = copyCount[Count](true, raw) // want "construct protection.Count through NewCount"
}

type box[T, R ~int] struct{ Value R }

func (value box[T, R]) Recast() T { return T(value.Value) }

type promotedRaw struct {
	Decoy Count
	box[Count, int]
}

type promotedCopy struct {
	Decoy int
	*box[Count, Count]
}

func PromotedTransfers(raw promotedRaw, valid promotedCopy) {
	_ = raw.Recast()            // want "construct protection.Count through NewCount"
	_ = promotedRaw.Recast(raw) // want "construct protection.Count through NewCount"
	_ = valid.Recast()
	_ = promotedCopy.Recast(valid)
	_ = raw.box.Recast() // want "construct protection.Count through NewCount"
	_ = valid.box.Recast()
}

func RangeTransfers(raw [][2]byte, keys map[[2]byte]int, channel <-chan [2]byte, iterator func(func([2]byte) bool), typed []ID, id ID) {
	for _, id = range raw { // want "construct protection.ID through NewID"
		consume(id)
	}
	for id = range keys { // want "construct protection.ID through NewID"
		consume(id)
	}
	for id = range channel { // want "construct protection.ID through NewID"
		consume(id)
	}
	for id = range iterator { // want "construct protection.ID through NewID"
		consume(id)
	}
	for _, id = range typed {
		consume(id)
	}
}

func GenericRangeTransfers[S ~[][2]byte, Yield ~func([2]byte) bool](raw S, iterator func(Yield), id ID) {
	for _, id = range raw { // want "construct protection.ID through NewID"
		consume(id)
	}
	for id = range iterator { // want "construct protection.ID through NewID"
		consume(id)
	}
}

func ElidedPointerTransfer(id ID) {
	_ = []*struct{ ID ID }{{ID: [2]byte{1}}} // want "construct protection.ID through NewID"
	_ = []*struct{ ID ID }{{ID: id}}
}

//domain:constructors NewOuter
type Outer struct{ Record } // want Outer:"requires constructors"

func NewOuter(record Record) Outer {
	outer := Outer{Record: record}
	outer.number = 2        // want "modify protection.Record through NewRecord"
	outer.Record.number = 3 // want "modify protection.Record through NewRecord"
	_ = outer.array[:]      // want "do not expose mutable contents of protection.Record"
	_ = &outer.number       // want "do not expose mutable contents of protection.Record"
	outer.Record = record
	return outer
}

//domain:constructors NewBox
type Box struct { // want Box:"requires constructors"
	Value any
	Count int
}

func NewBox(value any) Box { return Box{Value: value} }

func BoxedZeros() {
	_ = Box{Value: 0}     // want "construct protection.Box through NewBox"
	_ = Box{Value: false} // want "construct protection.Box through NewBox"
	_ = Box{nil, 0}
}
