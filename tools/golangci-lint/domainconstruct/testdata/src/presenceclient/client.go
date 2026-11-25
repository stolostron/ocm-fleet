package presenceclient

import "presenceflow"

func ImportedChecked(found bool, err error) {
	id, ok, err := presenceflow.LookupError(found, err)
	if err == nil && ok {
		presenceflow.Use(id)
	}
}

func ImportedUnchecked(found bool, err error) {
	id, _, err := presenceflow.LookupError(found, err)
	if err == nil {
		presenceflow.Use(id) // want "reason: unchecked-presence"
	}
}

func ImportedUncheckedError(found bool, err error) {
	id, ok, _ := presenceflow.LookupError(found, err)
	if ok {
		presenceflow.Use(id) // want "reason: unchecked-error"
	}
}

func ImportedInterface(provider presenceflow.Provider) {
	id, ok, err := provider.Lookup("key")
	if err == nil && ok {
		presenceflow.Use(id)
	}
}

func ImportedUncheckedInterface(provider presenceflow.Provider) {
	id, _, err := provider.Lookup("key")
	if err == nil {
		presenceflow.Use(id) // want "reason: unchecked-presence"
	}
}

func ImportedConcrete(registry presenceflow.Registry) {
	id, _, err := registry.Lookup("key")
	if err == nil {
		presenceflow.Use(id) // want "reason: unchecked-presence"
	}
}

//domain:presence
func ImportedForward(found bool, err error) (presenceflow.ID, bool, error) {
	return presenceflow.LookupError(found, err)
}

func ImportedBoxed() {
	value, _ := presenceflow.BoxedMissing()
	presenceflow.UseAny(value) // want "reason: unchecked-presence"
}

//domain:presence
func ForwardBoxedLookup(value presenceflow.ID, found bool) (any, bool) {
	return presenceflow.BoxedLookup(value, found)
}

func DiscardBoxedLookupPresence(value presenceflow.ID, found bool) {
	direct, _ := presenceflow.BoxedLookup(value, found)
	presenceflow.UseAny(direct)
	forwarded, _ := ForwardBoxedLookup(value, found)
	presenceflow.UseAny(forwarded)
}

func ImportedGeneric(found bool) {
	id, ok := presenceflow.GenericLookup(presenceflow.NewID("present"), found)
	if ok {
		presenceflow.Use(id)
	}
}

func ImportedGenericInvalid() {
	id, ok := presenceflow.GenericInvalid[presenceflow.ID]()
	if ok {
		presenceflow.Use(id) // want "reason: zero-value"
	}
}
