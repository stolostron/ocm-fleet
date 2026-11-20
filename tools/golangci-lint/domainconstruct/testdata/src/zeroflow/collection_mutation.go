package zeroflow

type projectedRow struct{ ID ID }

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
