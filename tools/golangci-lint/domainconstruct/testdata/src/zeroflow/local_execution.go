package zeroflow

import "errors"

func CopyInspectsOnlyReadSlots(valid ID, use func(ID)) {
	source := []ID{valid, ""}
	destination := make([]ID, len(source))
	copy(destination, source)
	use(destination[0])
	use(destination[1]) // want "argument contains an empty zeroflow.ID"
}

func CopyAllowsLaterRepair(valid ID, use func([]ID)) {
	source := []ID{"", valid}
	destination := make([]ID, len(source))
	copy(destination, source)
	destination[0] = valid
	use(destination)
}

func CopyToEmptyConsumesNothing(destination []ID) {
	copy(destination[:0], make([]ID, 1))
}

func EqualBoundsConsumeNothing(index int, use func([]ID)) {
	values := make([]ID, 1)
	use(values[index:index])
}

func DynamicCopyCanClobber(valid ID, count int, use func([]ID)) {
	destination := []ID{valid}
	copy(destination, make([]ID, count))
	use(destination) // want "argument may contain an empty zeroflow.ID"
}

func AppendDetachmentCopiesAtCall(valid ID, use func([]ID)) {
	values := make([]ID, 1, 1)
	result := append(values, valid)
	values[0] = valid
	use(result) // want "argument contains an empty zeroflow.ID"
}

func AppendReusesSpareCapacity(valid ID, use func([]ID)) {
	values := []ID{valid, valid}
	_ = append(values[:1], "")
	use(values) // want "argument contains an empty zeroflow.ID"
}

func AppendFullSliceIsDisjoint(valid ID, use func([]ID)) {
	values := []ID{valid, valid}
	_ = append(values[:1:1], "")
	use(values)
}

func DeferredClearDoesNotWriteYet(valid ID, use func([]ID)) {
	values := []ID{valid}
	defer clear(values)
	use(values)
}

func DeferredCopyDoesNotWriteYet(valid ID, use func([]ID)) {
	values := make([]ID, 1)
	defer copy(values, []ID{valid})
	use(values) // want "argument contains an empty zeroflow.ID"
}

func DeferredPointerReadsAtExit(valid ID, use func(*ID)) {
	var value ID
	defer use(&value)
	value = valid
}

func DeferredPointerStillZero(use func(*ID)) {
	var value ID
	defer use(&value) // want "argument is an empty zeroflow.ID"
}

func writeValidRaw(dst *string) { *dst = "valid" }

func DeferredWriteDoesNotInitializeRaw() ID {
	var raw string
	defer writeValidRaw(&raw)
	return ID(raw) // want "return produces an empty zeroflow.ID"
}

func DeferredClearDoesNotChangeJoin() (ID, error) {
	errs := []error{errors.New("failure")}
	defer clear(errs)
	return "", errors.Join(errs...)
}

func JoinNeedsDefiniteMembership(length int) (ID, error) {
	errs := make([]error, length, 2)
	errs[:2][1] = errors.New("failure")
	return "", errors.Join(errs...) // want "return produces an empty zeroflow.ID"
}

func ShiftedFillDoesNotCoverView(valid ID, length int, use func([]ID)) {
	values := make([]ID, length+1)
	for i := range length {
		values[i] = valid
	}
	use(values[1:][:length]) // want "argument may contain an empty zeroflow.ID"
}
