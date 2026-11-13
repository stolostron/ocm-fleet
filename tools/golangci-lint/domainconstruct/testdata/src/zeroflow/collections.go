package zeroflow

func AcceptIDs([]ID)           {}
func AcceptArray([2]ID)        {}
func AcceptRequests([]Request) {}
func AcceptPointers([]*ID)     {}

func ZeroSlice() { AcceptIDs(make([]ID, 2)) } // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""

func ZeroArray() {
	var ids [2]ID
	AcceptArray(ids) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func OmittedArrayElement(valid ID) {
	AcceptArray([2]ID{valid}) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func FilledArray(valid ID) {
	var ids [2]ID
	ids[0], ids[1] = valid, valid
	AcceptArray(ids)
}

func FilledSlice(valid ID) {
	ids := make([]ID, 2)
	ids[0], ids[1] = valid, valid
	AcceptIDs(ids)
}

func PartialSlice(valid ID) {
	ids := make([]ID, 2)
	ids[0] = valid
	Accept(ids[0])
	Accept(ids[1]) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	AcceptIDs(ids) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func SliceView(valid ID) {
	ids := make([]ID, 3)
	ids[1] = valid
	AcceptIDs(ids[1:2])
	AcceptIDs(ids[1:3]) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func OptionalElements() {
	AcceptPointers(make([]*ID, 2))
	AcceptPointers([]*ID{new(ID)}) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func NestedElements() {
	AcceptRequests(make([]Request, 1)) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func EmptyCollections() {
	AcceptIDs(nil)
	AcceptIDs([]ID{})
	AcceptIDs(make([]ID, 0, 4))
}

func FilledRange(size int, valid ID) {
	ids := make([]ID, size)
	for index := range ids {
		ids[index] = valid
	}
	AcceptIDs(ids)
}

func FilledCount(size int, valid ID) {
	ids := make([]ID, size)
	for index := 0; index < len(ids); index++ {
		ids[index] = valid
	}
	AcceptIDs(ids)
}

func ParsedRange(raw []string) ([]ID, error) {
	ids := make([]ID, len(raw))
	for index, input := range raw {
		id, err := NewID(input)
		if err != nil {
			return nil, err
		}
		ids[index] = id
	}
	return ids, nil
}

func AssignedBeforeCheck(raw []string) ([]ID, error) {
	ids := make([]ID, len(raw))
	for index, input := range raw {
		var err error
		ids[index], err = NewID(input)
		if err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func SkippedRange(size int, skip bool, valid ID) {
	ids := make([]ID, size)
	for index := range ids {
		if skip {
			continue
		}
		ids[index] = valid
	}
	AcceptIDs(ids) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func BrokenRange(size int, stop bool, valid ID) {
	ids := make([]ID, size)
	for index := range ids {
		if stop {
			break
		}
		ids[index] = valid
	}
	AcceptIDs(ids) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func AppendedValues(raw []string) ([]ID, error) {
	ids := make([]ID, 0, len(raw))
	for _, input := range raw {
		id, err := NewID(input)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func AppendedZero(valid ID) {
	var id ID
	AcceptIDs(append([]ID{valid}, id)) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func VariadicZero() {
	var id ID
	AcceptVariadic(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func AcceptVariadic(...ID) {}

func CopiedValues(valid ID) {
	ids := make([]ID, 2)
	copy(ids, []ID{valid, valid})
	AcceptIDs(ids)
}

func PartiallyCopied(valid ID) {
	ids := make([]ID, 2)
	copy(ids, []ID{valid})
	AcceptIDs(ids) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func ClearedValues(valid ID) {
	ids := []ID{valid}
	clear(ids)
	AcceptIDs(ids) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func CallerOwnedWrite(ids []ID) {
	var id ID
	ids[0] = id // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func DynamicView(size, start int) {
	ids := make([]ID, size)
	AcceptIDs(ids[start:]) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func FilledFields(valid ID) {
	requests := make([]Request, 1)
	requests[0].ID = valid
	AcceptRequests(requests)
}

func ElementPointers(valid ID) {
	ids := make([]ID, 2)
	ids[0] = valid
	AcceptPointer(&ids[0])
	AcceptPointer(&ids[1]) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func ClearedInsideLoop(size int, valid ID) {
	ids := make([]ID, size)
	for index := range ids {
		ids[index] = valid
		clear(ids)
	}
	AcceptIDs(ids) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func CopiedDynamic(raw []ID) {
	ids := make([]ID, len(raw))
	copy(ids, raw)
	AcceptIDs(ids)
}

func ZeroValidElements() { AcceptAny(make([]Total, 2)) }

func LargeSparse(valid ID) {
	ids := make([]ID, 1<<30)
	ids[0] = valid
	AcceptIDs(ids) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func LoopCarriedSlice(initial, next []ID, repeat, skip bool) {
	ids := initial
	for repeat {
		Accept(ids[0])
		if skip {
			continue
		}
		ids = next
	}
}

func BranchFill(size int, alternate bool, first, second ID) {
	ids := make([]ID, size)
	for index := range ids {
		if alternate {
			ids[index] = first
			continue
		}
		ids[index] = second
	}
	AcceptIDs(ids)
}

func JoinedBranchFill(size int, alternate bool, valid ID) {
	ids := make([]ID, size)
	for index := range ids {
		if alternate {
			ids[index] = valid
		} else {
			ids[index] = valid
		}
	}
	AcceptIDs(ids)
}

type CollectionInput struct{ values []string }

func FieldLength(input *CollectionInput, valid ID) {
	ids := make([]ID, len(input.values))
	for index := range input.values {
		ids[index] = valid
	}
	AcceptIDs(ids)
}

func ChangedFieldLength(input *CollectionInput, valid ID) {
	if len(input.values) == 0 {
		return
	}
	ids := make([]ID, len(input.values))
	input.values = input.values[:len(input.values)-1]
	for index := range input.values {
		ids[index] = valid
	}
	AcceptIDs(ids) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func ValueFieldLength(input CollectionInput, valid ID) {
	ids := make([]ID, len(input.values))
	for index := range input.values {
		ids[index] = valid
	}
	AcceptIDs(ids)
}

func NestedFilled(size int, valid ID) {
	envelope := OptionalEnvelope{Values: make([]ID, size)}
	for index := range envelope.Values {
		envelope.Values[index] = valid
	}
	AcceptEnvelope(envelope)
}

func NestedCleared(valid ID) {
	envelope := OptionalEnvelope{Values: []ID{valid}}
	clear(envelope.Values)
	AcceptEnvelope(envelope) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func InitializedPointerElements(valid ID) {
	id := new(ID)
	ids := []*ID{id}
	*id = valid
	AcceptPointers(ids)
}

func ClearEmpty(ids []ID) {
	clear(ids[:0])
	envelope := OptionalEnvelope{}
	clear(envelope.Values)
}
