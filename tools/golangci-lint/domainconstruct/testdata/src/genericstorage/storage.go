package genericstorage

//domain:constructors NewID
type ID string

func NewID(raw string) ID { return ID(raw) }

func OverwriteThroughArrayPointer[P ~*[1]ID](valid ID) []ID {
	values := []ID{valid}
	P(values)[0] = ""
	return values // want "return contains an empty genericstorage.ID"
}

func DisjointArrayPointer[P ~*[1]ID](valid ID) []ID {
	values := []ID{valid, valid}
	P(values[1:])[0] = ""
	return values[:1]
}

func StoreWithMixedArrayLengths[P ~*[1]ID | ~*[2]ID](values []ID, valid ID) {
	P(values)[0] = valid
}

func MakeUnfilledSlice[S ~[]ID]() S {
	return make(S, 1) // want "return contains an empty genericstorage.ID"
}

func MakeFilledSlice[S ~[]ID](valid ID) S {
	values := make(S, 1)
	values[0] = valid
	return values
}

func ClearBorrowedSlice[S ~[]ID](values S) {
	clear(values) // want "slice element after clear is an empty genericstorage.ID"
}

func ClearBorrowedMap[M ~map[int]ID](values M) {
	clear(values)
}
