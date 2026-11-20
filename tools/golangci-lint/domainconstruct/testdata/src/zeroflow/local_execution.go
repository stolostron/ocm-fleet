package zeroflow

import (
	"errors"
	"iter"
)

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

func DeferredDeleteChecksCapturedKey(values map[ID]bool, valid ID) {
	defer delete(values, valid)
	defer delete(values, ID("")) // want "argument is an empty zeroflow.ID"
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

func DeferredSliceReadsAtEarlyExit(early bool, valid ID, use func([]ID)) {
	values := make([]ID, 1)
	defer use(values) // want "argument contains an empty zeroflow.ID"
	if early {
		return
	}
	values[0] = valid
}

func DeferredSliceInitializedOnAllExits(early bool, valid ID, use func([]ID)) {
	values := make([]ID, 1)
	defer use(values)
	if early {
		values[0] = valid
		return
	}
	values[0] = valid
}

func DeferredSliceRegisteredInsideLoop(length int, valid ID, use func([]ID)) {
	for index := 0; index < length; index++ {
		values := make([]ID, 1)
		defer use(values)
		values[0] = valid
	}
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

func DeferredClearChangesReturnedCollection(valid ID) []ID {
	values := []ID{valid}
	defer clear(values)
	return values // want "return contains an empty zeroflow.ID"
}

func DeferredDisjointClear(valid ID) []ID {
	values := []ID{valid, valid}
	defer clear(values[1:])
	return values[:1]
}

func DeferredCopyReadsAfterEarlierCleanup(valid ID) []ID {
	source := []ID{valid}
	destination := []ID{valid}
	defer copy(destination, source)
	defer clear(source)
	return destination // want "return contains an empty zeroflow.ID"
}

func eraseScalar(raw *string) { *raw = "" }
func unrelatedCleanup()       {}

func DeferredWriteInOrderedCleanup(valid ID) (result ID) {
	result = valid
	defer eraseScalar((*string)(&result))
	defer unrelatedCleanup()
	return // want "return produces an empty zeroflow.ID"
}

func ConditionalDeferredZero(erase bool, valid ID) (result ID) {
	result = valid
	if erase {
		defer eraseScalar((*string)(&result))
	}
	return // want "return may produce an empty zeroflow.ID"
}

func DeferredZeroCannotReachEarlyReturn(early bool, valid ID) (result ID) {
	result = valid
	if early {
		return
	}
	defer eraseScalar((*string)(&result))
	return // want "return produces an empty zeroflow.ID"
}

func DeferredUseAfterRepair(use func(*ID)) {
	var raw string
	defer use((*ID)(&raw))
	defer writeValidRaw(&raw)
}

func DeferredUseBeforeRepair(use func(*ID)) {
	var raw string
	defer writeValidRaw(&raw)
	defer use((*ID)(&raw)) // want "argument is an empty zeroflow.ID"
}

func ConditionalDeferredReference(enabled bool, use func(*ID)) {
	var id ID
	if enabled {
		defer use(&id) // want "argument .*empty zeroflow.ID"
	}
}

func RepairBeforeConditionalCleanup(enabled bool, valid ID, use func(*ID)) {
	var id ID
	if enabled {
		defer use(&id)
	}
	id = valid
}

func InitializeOnRegistrationPath(enabled bool, valid ID, use func(*ID)) {
	var id ID
	if enabled {
		defer use(&id)
	}
	if enabled {
		id = valid
	}
}

func ConditionalDeferInOrderedPair(enabled bool, use func([]ID)) {
	values := make([]ID, 1)
	defer unrelatedCleanup()
	if enabled {
		defer use(values) // want "argument contains an empty zeroflow.ID"
	}
}

func RepairConditionalDeferInOrderedPair(enabled bool, valid ID, use func([]ID)) {
	values := make([]ID, 1)
	defer unrelatedCleanup()
	if enabled {
		defer use(values)
	}
	if enabled {
		values[0] = valid
	}
}

func IteratorFailureClearedAtExit(values iter.Seq[bool], valid ID) (id ID, err error) {
	defer func() { err = nil }()
	for range values {
		return "", errors.New("failure") // want "return may produce an empty zeroflow.ID"
	}
	return valid, nil
}

func setIteratorFlag(flag *bool) { *flag = true }

func IteratorFlagWrittenIndirectly(values iter.Seq[ID], skip bool, use func(ID)) {
	var id ID
	found := false
	for value := range values {
		if skip {
			setIteratorFlag(&found)
			continue
		}
		id, found = value, true
	}
	if found {
		use(id) // want "argument may be an empty zeroflow.ID"
	}
}

func IteratorOverwritesYieldResult(valid ID, use func(ID)) {
	var id ID
	found := false
	values := func(yield func(ID) bool) {
		yield(valid)
		id = "" // want "assignment stores an empty zeroflow.ID"
	}
	for value := range values {
		id, found = value, true
	}
	if found {
		use(id) // want "argument may be an empty zeroflow.ID"
	}
}

func IteratorFailureWithUnrelatedDefer(values iter.Seq[bool], valid ID) (ID, error) {
	defer unrelatedCleanup()
	for range values {
		return "", errors.New("failure")
	}
	return valid, nil
}

func IteratorFlagFromPreviousPass(first, second iter.Seq[ID], use func(ID)) {
	found := false
	for _, values := range []iter.Seq[ID]{first, second} {
		var id ID
		for value := range values {
			id = value
			found = true
		}
		if found {
			use(id) // want "argument may be an empty zeroflow.ID"
		}
	}
}

func IteratorFreshFlag(first, second iter.Seq[ID], use func(ID)) {
	for _, values := range []iter.Seq[ID]{first, second} {
		found := false
		var id ID
		for value := range values {
			id = value
			found = true
		}
		if found {
			use(id)
		}
	}
}

func IteratorResetFlag(first, second iter.Seq[ID], use func(ID)) {
	found := false
	for _, values := range []iter.Seq[ID]{first, second} {
		found = false
		var id ID
		for value := range values {
			id = value
			found = true
		}
		if found {
			use(id)
		}
	}
}
