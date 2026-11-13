package client

import renamed "values"

const Copied = renamed.FixedID

const Derived = renamed.FixedID + "/child" // want "construct values.ID through NewID or RestoreID"

func ConstantReads(id renamed.ID) {
	accept(renamed.FixedID)
	accept(Copied)
	_ = string(renamed.FixedID)
	_ = id == renamed.FixedID
	accept(renamed.UntypedID) // want "construct values.ID through NewID or RestoreID"
}
