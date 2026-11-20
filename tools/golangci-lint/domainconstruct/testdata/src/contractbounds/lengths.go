package contractbounds

func RecoveredSlice[T any](fail bool) []T {
	defer func() { _ = recover() }()
	if fail {
		panic("skipped allocation")
	}
	return make([]T, 1)
}

func PlainSlice[T any]() []T { return make([]T, 1) }
