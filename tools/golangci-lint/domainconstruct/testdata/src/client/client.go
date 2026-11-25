package client

import renamed "values"

type LocalID = renamed.ID

func Check(raw string, id renamed.ID, count renamed.Count, ref renamed.Ref) {
	_, _ = renamed.NewID(raw)
	_ = renamed.RestoreID(raw)
	_ = renamed.ID(raw) // want "construct values.ID through NewID or RestoreID"
	_ = LocalID(raw)    // want "construct values.ID through NewID or RestoreID"
	accept("unchecked") // want "construct values.ID through NewID or RestoreID"
	const unchecked = "unchecked"
	accept(unchecked)                  // want "construct values.ID through NewID or RestoreID"
	_ = []renamed.ID{"unchecked"}      // want "construct values.ID through NewID or RestoreID"
	_ = map[renamed.ID]int{"bad": 1}   // want "construct values.ID through NewID or RestoreID"
	_ = map[int]renamed.ID{1: "bad"}   // want "construct values.ID through NewID or RestoreID"
	_ = struct{ ID renamed.ID }{"bad"} // want "construct values.ID through NewID or RestoreID"
	_ = &renamed.Ref{}
	_ = []renamed.Ref{{}}
	_ = []*renamed.Ref{{}}
	_ = id + id                        // want "construct values.ID through NewID or RestoreID"
	id += "/child"                     // want "construct values.ID through NewID or RestoreID"
	count--                            // want "construct values.Count through NewCount"
	_ = count * count                  // want "construct values.Count through NewCount"
	_ = (*string)(&id)                 // want "expose mutable representation of values.ID"
	_ = (*renamed.ID)(&raw)            // want "construct values.ID through NewID or RestoreID"
	_ = ref
	accept(id)
	_ = string(id)
	_ = []renamed.ID{id}
	_ = map[renamed.ID]int{id: 1}
}

func accept(renamed.ID) {}

func badReturn() renamed.ID {
	return "unchecked" // want "construct values.ID through NewID or RestoreID"
}

// A similarly named function in another package receives no permission.
func NewID(raw string) renamed.ID {
	return renamed.ID(raw) // want "construct values.ID through NewID or RestoreID"
}

func refError(err error) (renamed.Ref, error) { return renamed.Ref{}, err }

func scalarError(err error) (renamed.ID, error) { return "", err }

// Following zero storage through these paths is the separate flow checker's job.
func deferredZeroFlow() renamed.ID {
	var storage renamed.ID
	return storage
}

func genericBypass(raw string) renamed.ID {
	return renamed.RawCast[renamed.ID](raw) // want "construct values.ID through NewID or RestoreID"
}
