package zeroflow

func CheckedRawLookup(values map[string]string, use func(ID)) {
	if raw, ok := values["key"]; ok {
		use(ID(raw)) // want "argument may be an empty zeroflow.ID"
	}
	if raw, ok := values["key"]; ok && raw != "" {
		use(ID(raw))
	}
}

func CheckedTypedLookup(values map[string]ID, use func(ID)) {
	if value, ok := values["key"]; ok {
		use(value)
	}
}

func StringSliceLengths(raw string, size int, use func(ID)) {
	use(ID(raw[:0]))        // want "argument is an empty zeroflow.ID"
	use(ID(raw[size:size])) // want "argument is an empty zeroflow.ID"
	use(ID(raw[:1]))
}

func implicitRuneZero[T ~string]() T {
	var value rune
	return T(value)
}

func implicitStringZero[T ~string]() T {
	var value string
	return T(value)
}

func ConsumeImplicitZero(use func(ID)) {
	use(implicitRuneZero[ID]())
	use(implicitStringZero[ID]()) // want "argument is an empty zeroflow.ID"
}
