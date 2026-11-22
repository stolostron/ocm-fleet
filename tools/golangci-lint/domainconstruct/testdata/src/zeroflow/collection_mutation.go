package zeroflow

import "errors"

type projectedRow struct{ ID ID }

func ReturnAppendedBoxedZero() [1]any {
	var slots [1]any
	_ = append(slots[:0], ID(""))
	return slots // want "return contains an empty zeroflow.ID"
}

func ReturnClearedAppendedPointee(valid ID) [1]any {
	var slots [1]any
	_ = append(slots[:0], &valid)
	valid = ""
	return slots // want "return contains an empty zeroflow.ID"
}

func AppendWithoutChangingOriginal(valid ID) [1]any {
	var slots [1]any
	_ = append(slots[:0:0], &valid)
	valid = ""
	return slots
}

func FillLoopThenClearPointee(valid ID) []*ID {
	pointer := new(ID)
	*pointer = valid
	values := make([]*ID, 1)
	for index := range values {
		values[index] = pointer
	}
	*pointer = ""
	return values // want "return may contain an empty zeroflow.ID"
}

func FillLoopWithValidPointee(valid ID) []*ID {
	pointer := new(ID)
	*pointer = valid
	values := make([]*ID, 1)
	for index := range values {
		values[index] = pointer
	}
	return values
}

func FillLoopKeepsCopiedValue(valid ID) []ID {
	pointer := new(ID)
	*pointer = valid
	values := make([]ID, 1)
	for index := range values {
		values[index] = *pointer
	}
	*pointer = ""
	return values
}

func EmptyIndexedString() ID {
	values := []string{""}
	return ID(values[0]) // want "return produces an empty zeroflow.ID"
}

func NonemptyIndexedString() ID {
	values := []string{"valid"}
	return ID(values[0])
}

type pointerRow struct{ Pointer *ID }

func CopyIntoZeroPointerField(id ID) *ID {
	source := []pointerRow{{Pointer: &id}}
	destination := make([]pointerRow, 1)
	copy(destination, source)
	id = ""
	return destination[0].Pointer // want "return produces an empty zeroflow.ID"
}

func CopyIntoPopulatedPointerField(id ID) *ID {
	other := id
	source := []pointerRow{{Pointer: &id}}
	destination := []pointerRow{{Pointer: &other}}
	copy(destination, source)
	id = ""
	return destination[0].Pointer // want "return produces an empty zeroflow.ID"
}

func CopyRetainsOriginalPointer(id ID) *ID {
	source := []pointerRow{{Pointer: &id}}
	destination := make([]pointerRow, 1)
	copy(destination, source)
	source[0].Pointer = new(ID)
	return destination[0].Pointer
}

func SliceArrayPointer(valid ID) *[1]ID {
	values := []ID{valid, ""}
	return (*[1]ID)(values[1:]) // want "return .*empty zeroflow.ID"
}

func SliceArrayCopyBeforeRepair(valid ID) [1]ID {
	values := []ID{valid, ""}
	snapshot := [1]ID(values[1:])
	values[1] = valid
	return snapshot // want "return .*empty zeroflow.ID"
}

func SliceArrayCopyBeforeOverwrite(valid ID) [1]ID {
	values := []ID{"", valid}
	snapshot := [1]ID(values[1:])
	values[1] = ""
	return snapshot
}

func WriteThroughSliceArrayPointer(valid ID) []ID {
	values := []ID{valid, valid}
	view := (*[1]ID)(values[1:])
	view[0] = ""
	return values // want "return .*empty zeroflow.ID"
}

func ReplaceSliceArrayRegion(valid ID) []ID {
	values := []ID{valid, valid}
	*(*[1]ID)(values[1:]) = [1]ID{}
	return values // want "return .*empty zeroflow.ID"
}

func ReplaceDisjointSliceArrayRegion(valid ID) []ID {
	values := []ID{valid, valid}
	*(*[1]ID)(values[1:]) = [1]ID{}
	return values[:1]
}

func clearArrayError(statuses *[1]error) { statuses[0] = nil }

func JoinAfterSliceArrayMutation() (ID, error) {
	statuses := []error{nil, errors.New("failure")}
	clearArrayError((*[1]error)(statuses[1:]))
	return "", errors.Join(statuses...) // want "return produces an empty zeroflow.ID"
}

func JoinAfterDisjointSliceArrayMutation() (ID, error) {
	statuses := []error{nil, errors.New("failure")}
	clearArrayError((*[1]error)(statuses[:1]))
	return "", errors.Join(statuses[1:]...)
}

func PossibleClearField(n int, valid ID, use func(ID)) {
	rows := []projectedRow{{valid}}
	clear(rows[:n])
	use(rows[0].ID) // want "argument may be an empty zeroflow.ID"
}

func ClearDoesNotReachField(n int, valid ID, use func(ID)) {
	rows := []projectedRow{{valid}, {valid}}
	if n == 0 {
		clear(rows[:n])
		use(rows[0].ID)
	}
	clear(rows[1:])
	use(rows[0].ID)
}

func CopyFields(valid ID, use func(ID)) {
	rows := []projectedRow{{valid}, {valid}}
	copy(rows, make([]projectedRow, 1))
	use(rows[0].ID) // want "argument is an empty zeroflow.ID"
	use(rows[1].ID)
	rows[0].ID = valid
	use(rows[0].ID)
}

func RepairFieldsWithCopy(valid ID, use func(ID)) {
	rows := make([]projectedRow, 1)
	copy(rows, []projectedRow{{valid}})
	use(rows[0].ID)
}

func ReadFieldAfterDynamicStore(valid ID, index int, use func(ID)) {
	rows := []struct{ Other, ID ID }{{valid, valid}, {valid, valid}}
	rows[index].ID = ""
	use(rows[0].ID) // want "argument may be an empty zeroflow.ID"
	use(rows[0].Other)
}

func ReturnCollectionAfterDynamicFieldStore(valid ID, index int) []projectedRow {
	rows := []projectedRow{{ID: valid}, {ID: valid}}
	rows[index].ID = ""
	return rows // want "return may contain an empty zeroflow.ID at path .*ID"
}

func ReturnCollectionAfterRepairingDynamicField(valid ID, index int) []projectedRow {
	rows := []projectedRow{{ID: valid}, {ID: valid}}
	rows[index].ID = ""
	rows[0].ID = valid
	return rows[:1]
}

func ReturnCollectionAfterDisjointFieldStore(valid ID) []projectedRow {
	rows := []projectedRow{{ID: valid}, {ID: valid}}
	rows[1].ID = ""
	return rows[:1]
}

func ReadFieldAfterDynamicElementStore(valid ID, index int, use func(ID)) {
	rows := []projectedRow{{valid}, {valid}}
	rows[index] = projectedRow{}
	use(rows[0].ID) // want "argument may be an empty zeroflow.ID"
}

func ReadFieldAfterAppend(valid ID, use func(ID)) {
	rows := []projectedRow{{valid}, {valid}}
	_ = append(rows[:1], projectedRow{})
	use(rows[0].ID)
	use(rows[1].ID) // want "argument is an empty zeroflow.ID"
}

func ReadFieldAfterAppendAllocates(valid ID, use func(ID)) {
	rows := []projectedRow{{valid}}
	_ = append(rows[:0:0], projectedRow{})
	use(rows[0].ID)
}

func CopyBranchSource(valid ID, replace bool) []ID {
	destination, source := []ID{valid}, []ID{valid}
	if replace {
		source = make([]ID, 1)
	}
	copy(destination, source)
	return destination // want "return may contain an empty zeroflow.ID"
}

func CopyValidBranchSources(first, second ID, replace bool) []ID {
	destination, source := []ID{first}, []ID{first}
	if replace {
		source = []ID{second}
	}
	copy(destination, source)
	return destination
}

func ReadFieldFromCopyOfBranchSource(valid ID, replace bool, use func(ID)) {
	destination, source := []projectedRow{{valid}}, []projectedRow{{valid}}
	if replace {
		source = make([]projectedRow, 1)
	}
	copy(destination, source)
	use(destination[0].ID) // want "argument may be an empty zeroflow.ID"
}

func CopyBranchSourceKeepsUnwrittenSlot(valid ID, skip bool) []ID {
	destination, source := make([]ID, 1), []ID{valid}
	if skip {
		source = nil
	}
	copy(destination, source)
	return destination // want "return .* an empty zeroflow.ID"
}

func CopyEmptyBranchSource(valid ID, skip bool) []ID {
	destination, source := []ID{valid}, []ID{valid}
	if skip {
		source = nil
	}
	copy(destination, source)
	return destination
}

func eraseFirstString(values []*string) { *values[0] = "" }

func CallAfterCopyingPointer(raw *string, use func(ID)) {
	source := []*string{raw}
	values := make([]*string, 1)
	copy(values, source)
	other := "other"
	source[0] = &other
	if *raw != "" {
		eraseFirstString(values)
		use(ID(*raw)) // want "argument may be an empty zeroflow.ID"
	}
}

func CallWithDisjointCopiedPointer(raw *string, use func(ID)) {
	values := make([]*string, 2)
	other := "other"
	values[0] = &other
	copy(values[1:], []*string{raw})
	if *raw != "" {
		eraseFirstString(values[:1:1])
		use(ID(*raw))
	}
}

func orderedStores(first, second *string) {
	*first = ""
	*second = "valid"
}

func SameStorageEffect() ID {
	raw := "valid"
	orderedStores(&raw, &raw)
	return ID(raw)
}

func LastWriteWins() ID {
	raw := "valid"
	clearLast(&raw, &raw)
	return ID(raw) // want "return produces an empty zeroflow.ID"
}

func clearLast(first, second *string) {
	*first = "valid"
	*second = ""
}

func DisjointStorageEffects() ID {
	raw, other := "valid", "valid"
	orderedStores(&raw, &other)
	return ID(raw) // want "return produces an empty zeroflow.ID"
}

func BorrowedScalarStore(first, second *string, use func(ID)) {
	*first = "valid"
	*second = ""
	use(ID(*first)) // want "argument may be an empty zeroflow.ID"
}

func GuardedDisjointScalarStore(first, second *string, use func(ID)) {
	if first != second {
		*first = "valid"
		*second = ""
		use(ID(*first))
	}
	*second = ""
	*first = "valid"
	use(ID(*first))
}

func ConditionalCleanupCopy(erase bool, valid ID) []ID {
	destination, source := []ID{valid}, []ID{valid}
	defer copy(destination, source)
	if erase {
		defer clear(source)
	}
	return destination // want "return may contain an empty zeroflow.ID"
}

func ConditionalCleanupDisjoint(erase bool, valid ID) []ID {
	destination, source, other := []ID{valid}, []ID{valid}, []ID{valid}
	defer copy(destination, source)
	if erase {
		defer clear(other)
	}
	return destination
}
