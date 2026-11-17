package optionalflow

import (
	"optionalhelpers"
	"optionalvalues"

	opt "github.com/stolostron/ocm-fleet/server/pkg/optional"
)

type Envelope struct {
	Name     opt.Optional[optionalvalues.ID]
	Required optionalvalues.ID
}

//domain:constructors NewCache
//domain:zero-valid
type Cache struct{ name opt.Optional[optionalvalues.ID] }

func NewCache() Cache { return Cache{} }

type Optional[T any] struct {
	Value   T
	Present bool
}

type Different opt.Optional[optionalvalues.ID]

func useEnvelope(Envelope)                                   {}
func useCache(Cache)                                         {}
func useAny(any)                                             {}
func useSlice([]opt.Optional[optionalvalues.ID])              {}
func useArray([2]opt.Optional[optionalvalues.ID])              {}
func useMap(map[string]opt.Optional[optionalvalues.ID])       {}
func usePointer(*opt.Optional[optionalvalues.ID])             {}
func useLookalike(Optional[optionalvalues.ID])                {}
func useDifferent(Different)                                 {}
func useDomainPointer(*optionalvalues.ID)                    {}
func useDomainSlice([]optionalvalues.ID)                     {}
func useDomainMap(map[string]optionalvalues.ID)              {}

func AbsentStorage() opt.Optional[optionalvalues.ID] {
	var value opt.Optional[optionalvalues.ID]
	optionalvalues.UseOptional(value)
	optionalvalues.UseOptional(opt.Optional[optionalvalues.ID]{})
	usePointer(new(opt.Optional[optionalvalues.ID]))
	useAny(value)
	useSlice(make([]opt.Optional[optionalvalues.ID], 2))
	useArray([2]opt.Optional[optionalvalues.ID]{})
	useMap(map[string]opt.Optional[optionalvalues.ID]{"missing": {}})
	useCache(Cache{})
	return value
}

func RequiredSibling() {
	useEnvelope(Envelope{}) // want "(?s)rule: domainzero[.]invalid-zero.*path: \"Required\""
	useEnvelope(Envelope{Required: optionalvalues.NewID("present")})
}

func UnrelatedTypes() {
	useLookalike(Optional[optionalvalues.ID]{}) // want "(?s)rule: domainzero[.]invalid-zero.*path: \"Value\""
	useDifferent(Different{}) // want "(?s)rule: domainzero[.]invalid-zero.*path: \"value\""
}

func Aliases() optionalvalues.Name {
	var value optionalvalues.Generic[optionalvalues.ID]
	optionalvalues.UseOptional(value)
	return optionalvalues.Name{}
}

func Checked(value opt.Optional[optionalvalues.ID]) {
	id, ok := value.Get()
	if ok {
		optionalvalues.Use(id)
	}
}

func EarlyReturn(value opt.Optional[optionalvalues.ID]) {
	id, ok := value.Get()
	if !ok {
		return
	}
	optionalvalues.Use(id)
}

func Unchecked(value opt.Optional[optionalvalues.ID]) {
	id, _ := value.Get()
	optionalvalues.Use(id) // want "reason: unchecked-presence"
}

func WithError(value opt.Optional[optionalvalues.ID], err error) (opt.Optional[optionalvalues.ID], error) {
	return value, err
}

func CheckedError(value opt.Optional[optionalvalues.ID], err error) {
	result, err := WithError(value, err)
	if err != nil {
		return
	}
	id, ok := result.Get()
	if ok {
		optionalvalues.Use(id)
	}
}

func UncheckedError(value opt.Optional[optionalvalues.ID], err error) {
	result, _ := WithError(value, err)
	id, ok := result.Get() // want "reason: unchecked-error"
	if ok {
		optionalvalues.Use(id)
	}
}

func FalseBranch(value opt.Optional[optionalvalues.ID]) {
	id, ok := value.Get()
	if !ok {
		optionalvalues.Use(id) // want "reason: unchecked-presence"
	}
}

func UnrelatedGuard(first, second opt.Optional[optionalvalues.ID]) {
	id, _ := first.Get()
	_, ok := second.Get()
	if ok {
		optionalvalues.Use(id) // want "reason: unchecked-presence"
	}
}

func ReassignedGuard(value opt.Optional[optionalvalues.ID]) {
	id, ok := value.Get()
	ok = true
	if ok {
		optionalvalues.Use(id) // want "reason: unchecked-presence"
	}
}

func CopySurvivesReplacement(value opt.Optional[optionalvalues.ID]) {
	id, ok := value.Get()
	value = opt.Optional[optionalvalues.ID]{}
	optionalvalues.UseOptional(value)
	if ok {
		optionalvalues.Use(id)
	}
}

func MethodValue(value opt.Optional[optionalvalues.ID]) {
	get := value.Get
	id, ok := get()
	if ok {
		optionalvalues.Use(id)
	}
	unchecked, _ := get()
	optionalvalues.Use(unchecked) // want "reason: unchecked-presence"
}

func MethodExpression(value opt.Optional[optionalvalues.ID]) {
	id, ok := opt.Optional[optionalvalues.ID].Get(value)
	if ok {
		optionalvalues.Use(id)
	}
	unchecked, _ := opt.Optional[optionalvalues.ID].Get(value)
	optionalvalues.Use(unchecked) // want "reason: unchecked-presence"
}

func InvalidSome() {
	var zero optionalvalues.ID
	optionalvalues.UseOptional(opt.Some(zero)) // want "rule: domainzero[.]invalid-zero"
	optionalvalues.UseOptional(opt.Some[optionalvalues.ID]("")) // want "rule: domainzero[.]invalid-zero"
	optionalvalues.UseRecordOptional(opt.Some(optionalvalues.Record{})) // want "(?s)rule: domainzero[.]invalid-zero.*path: \"ID\""
}

func PresentValues(valid optionalvalues.ID) {
	optionalvalues.UseOptional(opt.Some(valid))
	useAny(opt.Some[optionalvalues.Total](0))
	useAny(opt.Some[*optionalvalues.ID](nil))
	useAny(opt.Some[any](nil))
	useAny(opt.Some(opt.Optional[optionalvalues.ID]{}))
}

func MapAndSlice(values map[string]opt.Optional[optionalvalues.ID], list []opt.Optional[optionalvalues.ID]) {
	value := values["possibly-missing"]
	id, ok := value.Get()
	if ok {
		optionalvalues.Use(id)
	}
	unchecked, _ := list[0].Get()
	optionalvalues.Use(unchecked) // want "reason: unchecked-presence"
}

func CollectionUpdates(values []opt.Optional[optionalvalues.ID]) {
	values = append(values, opt.Optional[optionalvalues.ID]{})
	copy(values, make([]opt.Optional[optionalvalues.ID], 2))
	clear(values)
	useSlice(values)
}

func ChannelAndAssertion(values <-chan opt.Optional[optionalvalues.ID], boxed any) {
	value := <-values
	optionalvalues.UseOptional(value)
	asserted, _ := boxed.(opt.Optional[optionalvalues.ID])
	optionalvalues.UseOptional(asserted)
	id, ok := asserted.Get()
	if ok {
		optionalvalues.Use(id)
	}
}

func ZeroSafePayloads(pointer opt.Optional[*optionalvalues.ID], slice opt.Optional[[]optionalvalues.ID], entries opt.Optional[map[string]optionalvalues.ID]) {
	gotPointer, _ := pointer.Get()
	useDomainPointer(gotPointer)
	gotSlice, _ := slice.Get()
	useDomainSlice(gotSlice)
	gotMap, _ := entries.Get()
	useDomainMap(gotMap)
}

func OptionalArrayPayload(value opt.Optional[[2]optionalvalues.ID]) {
	ids, ok := value.Get()
	if ok {
		optionalvalues.Use(ids[0])
	}
	unchecked, _ := value.Get()
	optionalvalues.Use(unchecked[0]) // want "reason: unchecked-presence"
}

func InvalidPointerSome() {
	var id optionalvalues.ID
	useAny(opt.Some(&id)) // want "rule: domainzero[.]invalid-zero"
}

func Nested(value opt.Optional[opt.Optional[optionalvalues.ID]]) {
	inner, _ := value.Get()
	id, ok := inner.Get()
	if ok {
		optionalvalues.Use(id)
	}
}

func GenericFactories(valid optionalvalues.ID) {
	optionalvalues.UseOptional(optionalhelpers.Absent[optionalvalues.ID]())
	optionalvalues.UseOptional(optionalhelpers.NamedAbsent[optionalvalues.ID]())
	optionalvalues.UseOptional(optionalhelpers.Wrap(valid))
	optionalvalues.UseOptional(optionalhelpers.PresentZero[optionalvalues.ID]()) // want "rule: domainzero[.]invalid-zero"
	optionalvalues.UseOptional(optionalhelpers.MaybeZero[optionalvalues.ID](true)) // want "rule: domainzero[.]invalid-zero"
}

//domain:presence
func Forward(value opt.Optional[optionalvalues.ID]) (optionalvalues.ID, bool) {
	return value.Get()
}

func GenericForward(value opt.Optional[optionalvalues.ID]) {
	id, ok := optionalhelpers.Forward(value)
	if ok {
		optionalvalues.Use(id)
	}
	unchecked, _ := optionalhelpers.Forward(value)
	optionalvalues.Use(unchecked) // want "reason: unchecked-presence"
}
