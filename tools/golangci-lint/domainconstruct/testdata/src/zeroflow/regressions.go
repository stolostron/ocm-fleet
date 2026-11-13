package zeroflow

import (
	"fmt"
	"iter"
	"testing"

	"flowhelpers"
)

func localIndexes(size int) []int {
	indexes := make([]int, size)
	for index := range indexes {
		indexes[index] = index
	}
	return indexes
}

func FilledFromLocalHelper(size int, valid ID) []ID {
	indexes := localIndexes(size)
	ids := make([]ID, size)
	for index := range indexes {
		ids[index] = valid
	}
	return ids
}

func FilledFromImportedHelper(size int, valid ID) []ID {
	indexes := flowhelpers.Indexes(size)
	ids := make([]ID, size)
	for index := range indexes {
		ids[index] = valid
	}
	return ids
}

func FilledFromMethodHelper(cursor *flowhelpers.Cursor, size int, valid ID) []Request {
	indexes := cursor.Next(size)
	requests := make([]Request, size)
	for index := range indexes {
		requests[index] = Request{ID: valid}
	}
	return requests
}

func PartialFromTruncatedHelper(size int, valid ID) []ID {
	ids := make([]ID, size)
	for index := range flowhelpers.Truncated(size) {
		ids[index] = valid
	}
	return ids // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func PartialFromChangedParameter(size int, valid ID) []ID {
	ids := make([]ID, size)
	for index := range flowhelpers.ChangedParameter(size) {
		ids[index] = valid
	}
	return ids // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func PartialFromMixedLengths(size int, shorten bool, valid ID) []ID {
	ids := make([]ID, size)
	for index := range flowhelpers.MixedLengths(size, shorten) {
		ids[index] = valid
	}
	return ids // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func PartialFromEscapedHeader(size int, valid ID) []ID {
	ids := make([]ID, size)
	for index := range flowhelpers.EscapedHeader(size) {
		ids[index] = valid
	}
	return ids // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func PartialFromCapturedHeader(size int, valid ID) []ID {
	ids := make([]ID, size)
	for index := range flowhelpers.CapturedHeader(size) {
		ids[index] = valid
	}
	return ids // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func HelperFillWithSkippedSlot(size int, skip bool, valid ID) []ID {
	ids := make([]ID, size)
	for index := range flowhelpers.Indexes(size) {
		if skip {
			continue
		}
		ids[index] = valid
	}
	return ids // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func PartialFromConditionalHeader(size int, allocate bool, valid ID) []ID {
	ids := make([]ID, size)
	for index := range flowhelpers.ConditionalHeader(size, allocate) {
		ids[index] = valid
	}
	return ids // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func PartialFromEarlyHeaderReturn(size int, skip bool, valid ID) []ID {
	ids := make([]ID, size)
	for index := range flowhelpers.EarlyHeaderReturn(size, skip) {
		ids[index] = valid
	}
	return ids // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func TableErrorAssertion(t *testing.T, raw string, wantErr bool) {
	id, err := NewID(raw)
	if (err != nil) != wantErr {
		t.Fatalf("NewID error = %v, wantErr %v", err, wantErr)
	}
	if wantErr {
		return
	}
	Accept(id)
}

func TableSuccessAssertion(t testing.TB, raw string, wantSuccess bool) {
	id, err := NewID(raw)
	if (err == nil) != wantSuccess {
		t.Fatal("unexpected result")
	}
	if !wantSuccess {
		return
	}
	Accept(id)
}

type errorCase struct {
	raw     string
	wantErr bool
}

func CapturedTableCase(t *testing.T, cases []errorCase) {
	for _, test := range cases {
		t.Run(test.raw, func(t *testing.T) {
			id, err := NewID(test.raw)
			if (err != nil) != test.wantErr {
				t.Fatalf("NewID error = %v, wantErr %v", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			Accept(id)
		})
	}
}

func ChangedTableCase(t *testing.T, test *errorCase) {
	id, err := NewID(test.raw)
	if (err != nil) != test.wantErr {
		t.Fatal("unexpected error")
	}
	test.wantErr = false
	if test.wantErr {
		return
	}
	Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func MutatedTableCase(t *testing.T, test *errorCase, mutate func(*bool)) {
	id, err := NewID(test.raw)
	if (err != nil) != test.wantErr {
		t.Fatal("unexpected error")
	}
	mutate(&test.wantErr)
	if test.wantErr {
		return
	}
	Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func NonfatalTableAssertion(t *testing.T, raw string, wantErr bool) {
	id, err := NewID(raw)
	if (err != nil) != wantErr {
		t.Errorf("unexpected error: %v", err)
	}
	if wantErr {
		return
	}
	Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

type returningReporter struct{}

func (returningReporter) Fatalf(string, ...any) {}

func UnrelatedFatalf(raw string, wantErr bool) {
	id, err := NewID(raw)
	if (err != nil) != wantErr {
		returningReporter{}.Fatalf("unexpected error: %v", err)
	}
	if wantErr {
		return
	}
	Accept(id) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func ResolveSequence(aliases iter.Seq[ID], resolved map[ID]ID) (ID, error) {
	var target ID
	var found bool
	for alias := range aliases {
		name, ok := resolved[alias]
		if !ok {
			continue
		}
		if found && name != target {
			return "", fmt.Errorf("aliases disagree")
		}
		target, found = name, true
	}
	if !found {
		return "", fmt.Errorf("no alias resolved")
	}
	return target, nil
}

func IteratorZeroSuccess(aliases iter.Seq[ID]) (ID, error) {
	for range aliases {
		return "", nil // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	}
	return NewID("fallback")
}

func IteratorUnknownError(aliases iter.Seq[ID], err error) (ID, error) {
	for range aliases {
		return "", err // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	}
	return NewID("fallback")
}

func IteratorForward(raw iter.Seq[string]) (ID, error) {
	for input := range raw {
		return NewID(input)
	}
	return NewID("fallback")
}

func IteratorFlagWithoutInitialization(values iter.Seq[ID]) (ID, error) {
	var id ID
	var found bool
	for range values {
		found = true
	}
	if !found {
		return "", fmt.Errorf("not found")
	}
	return id, nil // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func IteratorFlagWithInvalidWrite(values iter.Seq[ID]) (ID, error) {
	var id ID
	var found bool
	for range values {
		id, found = "", true // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	}
	if !found {
		return "", fmt.Errorf("not found")
	}
	return id, nil // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func IteratorFlagEscapesBeforeLoop(values iter.Seq[ID], mutate func(*bool)) (ID, error) {
	var id ID
	var found bool
	mutate(&found)
	for value := range values {
		id, found = value, true
	}
	if !found {
		return "", fmt.Errorf("not found")
	}
	return id, nil // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

type callbackSpec struct{ start func(Request) }

func CallbackInitializes(invoke func(*callbackSpec)) {
	var captured Request
	spec := &callbackSpec{start: func(request Request) { captured = request }}
	invoke(spec)
	Accept(captured.ID) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: zero-initialized-storage.*type: \"zeroflow[.]ID\""
}

func UnusedCallbackDoesNotInitialize() {
	var captured Request
	_ = &callbackSpec{start: func(request Request) { captured = request }}
	Accept(captured.ID) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func UnrelatedCallDoesNotInitialize(invoke func(), keep func(*callbackSpec)) {
	var captured Request
	spec := &callbackSpec{start: func(request Request) { captured = request }}
	invoke()
	Accept(captured.ID) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
	keep(spec)
}

func ResetAfterCallback(invoke func(*callbackSpec)) {
	var captured Request
	spec := &callbackSpec{start: func(request Request) { captured = request }}
	invoke(spec)
	captured = Request{}
	Accept(captured.ID) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func CopyBeforeCallback(invoke func(*callbackSpec)) {
	var captured Request
	copy := captured
	spec := &callbackSpec{start: func(request Request) { captured = request }}
	invoke(spec)
	Accept(copy.ID) // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func CapturedErrorAssignedBeforeReturn() (id ID, err error) {
	_ = func() { err = nil }
	return "", fmt.Errorf("failure")
}

func DeferredErrorCanBecomeNil() (id ID, err error) {
	defer func() { err = nil }()
	return "", fmt.Errorf("failure") // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}

func PartialFromPointerReceiver(size int, valid ID) []ID {
	ids := make([]ID, size)
	for index := range flowhelpers.MethodShortened(size) {
		ids[index] = valid
	}
	return ids // want "(?s)rule: domainzero[.]invalid-zero.*type: \"zeroflow[.]ID\""
}
