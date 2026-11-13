package zeroflow

import "flowhelpers"

// StringScanner models an output API with variadic destinations.
type StringScanner interface{ Scan(...any) error }

func MutationIntoRaw(scan func(*string)) {
	var raw string
	scan(&raw)
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: zero-initialized-storage.*type: \"zeroflow[.]ID\""
}

func MutationThroughAlias(scan func(*string)) {
	var raw string
	alias := &raw
	scan(alias)
	AcceptRequest(Request{ID: ID(raw)}) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: zero-initialized-storage.*type: \"zeroflow[.]ID\"; field-path: \"ID\""
}

func MutationThroughVariadic(scanner StringScanner) error {
	var raw string
	if err := scanner.Scan(&raw); err != nil {
		return err
	}
	request := Request{ID: ID(raw)}
	AcceptRequest(request) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: zero-initialized-storage.*type: \"zeroflow[.]ID\"; field-path: \"ID\""
	return nil
}

func MutationThroughAppendedDestinations(scanner StringScanner) error {
	var raw, other string
	args := []any{&other}
	args = append(args, &raw)
	if err := scanner.Scan(args...); err != nil {
		return err
	}
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: zero-initialized-storage.*type: \"zeroflow[.]ID\""
	return nil
}

func MutationThroughField(invoke func(any)) {
	var raw string
	input := struct{ Destination *string }{Destination: &raw}
	invoke(input)
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: zero-initialized-storage.*type: \"zeroflow[.]ID\""
}

func KnownZeroAfterUnrelatedMutation(invoke func(*string)) {
	var raw, other string
	invoke(&other)
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
}

func CopyBeforeRawMutation(invoke func(*string)) {
	var raw string
	copy := raw
	invoke(&raw)
	Accept(ID(copy)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
}

func ResetAfterRawMutation(invoke func(*string)) {
	var raw string
	invoke(&raw)
	raw = ""
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
}

func CheckedAfterRawMutation(invoke func(*string)) error {
	var raw string
	invoke(&raw)
	id, err := NewID(raw)
	if err != nil {
		return err
	}
	Accept(id)
	return nil
}

func inspectRaw(raw *string) bool { return *raw == "" }

func assignEmpty(raw *string) { *raw = "" }

func assignNonempty(raw *string) { *raw = "nonempty" }

func ReadOnlyHelperPreservesZero() {
	var raw string
	_ = inspectRaw(&raw)
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
}

func HelperAssignsZero() {
	raw := "nonempty"
	assignEmpty(&raw)
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
}

func HelperAssignsNonzero() {
	var raw string
	assignNonempty(&raw)
	Accept(ID(raw))
}

func PrimitiveParameterMayBeZero(raw string) {
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: input-may-be-zero.*value: \"raw\".*type: \"zeroflow[.]ID\""
}

func GuardedPrimitiveParameter(raw string) {
	if raw == "" {
		return
	}
	Accept(ID(raw))
}

func TrustedDomainParameter(id ID) { Accept(id) }

func TrustedDomainFactory(produce func() ID) { Accept(produce()) }

func ValidZeroDomainConversion(total Total) NarrowID {
	return NarrowID(total) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible.*type: \"zeroflow[.]NarrowID\""
}

func ValidZeroDomainFactoryConversion(produce func() Total) NarrowID {
	return NarrowID(produce()) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible.*type: \"zeroflow[.]NarrowID\""
}

func GuardedValidZeroDomainConversion(total Total) NarrowID {
	if total == 0 {
		panic("empty ID")
	}
	return NarrowID(total)
}

func TrustedNonzeroDomainConversion(id InspectionID) { Accept(ID(id)) }

func ConditionalZero(valid ID, condition bool) {
	var id ID
	if condition {
		id = valid
	}
	Accept(id) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible.*type: \"zeroflow[.]ID\""
}

func assignWrapped(raw *string) { (*raw) = "" }

func WrappedHelperAssignsZero() {
	raw := "nonempty"
	assignWrapped(&raw)
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
}

func conditionallyAssign(raw *string, condition bool) {
	if condition {
		*raw = "nonempty"
	}
}

func ConditionalHelperMayLeaveZero(condition bool) {
	var raw string
	conditionallyAssign(&raw, condition)
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: zero-initialized-storage.*type: \"zeroflow[.]ID\""
}

func ImportedMutationContracts() {
	var raw string
	_ = flowhelpers.Inspect(&raw)
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
	flowhelpers.AssignNonempty(&raw)
	Accept(ID(raw))
	flowhelpers.AssignEmpty(&raw)
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
}

func PrimitiveFactoryMayBeZero(produce func() string) {
	Accept(ID(produce())) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible.*type: \"zeroflow[.]ID\""
}

func PrimitiveFieldMayBeZero(input struct{ Raw string }) {
	Accept(ID(input.Raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: input-may-be-zero.*value: \"input[.]Raw\".*type: \"zeroflow[.]ID\""
}

func NestedPrimitiveFieldMayBeZero(input struct{ Metadata struct{ Name string } }) {
	Accept(ID(input.Metadata.Name)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: input-may-be-zero.*value: \"input[.]Metadata[.]Name\".*type: \"zeroflow[.]ID\""
}

type rawResourceSpec struct{ Name string }

func (s rawResourceSpec) RawReceiverFieldMayBeZero() ID {
	return ID(s.Name) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: input-may-be-zero.*value: \"s[.]Name\".*type: \"zeroflow[.]ID\""
}

func GenericPrimitiveConversion(raw string) {
	Accept(flowhelpers.Convert[ID](raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible.*type: \"zeroflow[.]ID\""
}

func GenericMutationConversion(scan func(*string)) {
	var raw string
	scan(&raw)
	Accept(flowhelpers.Convert[ID](raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: zero-initialized-storage.*type: \"zeroflow[.]ID\""
}

func MutatedZeroElements(mutate func([]ID)) {
	ids := make([]ID, 2)
	mutate(ids)    // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
	AcceptIDs(ids) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: zero-initialized-storage.*type: \"zeroflow[.]ID\""
}

func MutatedZeroElementPointer(mutate func(*ID)) {
	ids := make([]ID, 2)
	mutate(&ids[1])    // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
	AcceptIDs(ids[1:]) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: zero-initialized-storage.*type: \"zeroflow[.]ID\""
	AcceptIDs(ids[:1]) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
}

func MutatedOtherRange(mutate func([]ID), valid ID) {
	ids := make([]ID, 2)
	ids[0] = valid
	mutate(ids[:1])
	AcceptIDs(ids[1:]) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
}

func ArrayCopyBeforeMutation(mutate func([]ID)) {
	ids := [1]ID{}
	copy := ids
	mutate(ids[:])  // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
	AcceptAny(copy) // want "(?s)rule: domainzero[.]invalid-zero; certainty: known.*type: \"zeroflow[.]ID\""
	AcceptAny(ids)  // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: zero-value.*type: \"zeroflow[.]ID\""
}

func ConditionalArrayZero(valid ID, condition bool) {
	ids := [1]ID{}
	if condition {
		ids[0] = valid
	}
	AcceptAny(ids) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible.*type: \"zeroflow[.]ID\""
}

func UnknownMutationPreservesValidDomain(mutate func(*ID), id ID) {
	mutate(&id)
	Accept(id)
}

func UnknownMutationPreservesValidCollection(mutate func([]ID), valid ID) {
	ids := []ID{valid}
	mutate(ids)
	AcceptIDs(ids)
}

func MutationPreservesRawInputSource(mutate func(*string), raw string) {
	mutate(&raw)
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: input-may-be-zero"
}

func UnknownMutationOfRawStorage(mutate func(*string)) {
	raw := "nonempty"
	mutate(&raw)
	Accept(ID(raw)) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: input-may-be-zero"
}

func MutationPreservesClearedSource(mutate func([]ID), valid ID) {
	ids := []ID{valid}
	clear(ids)
	mutate(ids)    // want "(?s)rule: domainzero[.]invalid-zero; certainty: known; reason: cleared-element"
	AcceptIDs(ids) // want "(?s)rule: domainzero[.]invalid-zero; certainty: possible; reason: cleared-element"
}
