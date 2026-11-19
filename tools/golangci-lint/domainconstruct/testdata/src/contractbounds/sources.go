package contractbounds

func duplicate[T any](value T) struct{ A, B any } {
	return struct{ A, B any }{value, value}
}

func SmallComposition[T any](value T) struct{ A, B any } {
	return duplicate(duplicate(value))
}

func LargeComposition[T any](value T) struct{ A, B any } {
	return duplicate(duplicate(duplicate(duplicate(duplicate(duplicate(duplicate(duplicate(duplicate(duplicate(duplicate(duplicate(value))))))))))))
}

func enclose[T any](value T) struct{ Inner any } {
	return struct{ Inner any }{value}
}

func AtSourceDepth[T any](value T) struct{ Inner any } {
	return enclose(enclose(enclose(enclose(enclose(enclose(enclose(enclose(value))))))))
}

func ChoiceBeyondSourceDepth[T any](first bool, value T) struct{ Inner any } {
	if first {
		return AtSourceDepth(value)
	}
	return struct{ Inner any }{}
}
