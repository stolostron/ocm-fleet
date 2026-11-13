package values

// FixedID illustrates a reviewed constant declaration. Its uses are copies.
const FixedID ID = "fixed" // want "construct values.ID through NewID or RestoreID"

const UntypedID = "unchecked"

func comparisons(id ID, count Count) bool {
	_ = id == "fixed"
	_ = "fixed" != id
	_ = id < "later"
	_ = count == 10
	_ = id == ("fi" + "xed")
	_ = id == ID("fixed") // want "construct values.ID through NewID or RestoreID"
	switch id {
	case "first", "second":
		return true
	}
	return id == FixedID
}

func explicitZeroStorage() {
	var id ID
	_ = id
	_ = ID("")
	_ = Ref{}
	_ = new(Ref)
	_ = &Ref{}
}
