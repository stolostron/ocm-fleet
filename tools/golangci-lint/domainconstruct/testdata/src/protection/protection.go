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
