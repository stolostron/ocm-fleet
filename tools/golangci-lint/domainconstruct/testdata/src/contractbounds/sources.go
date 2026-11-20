package contractbounds

func duplicate[T any](value T) struct{ A, B any } {
	return struct{ A, B any }{value, value}
}

func SmallComposition[T any](value T) struct{ A, B any } {
	return duplicate(duplicate(value))
}

func quadruple[T any](value T) struct{ A, B, C, D any } {
	return struct{ A, B, C, D any }{value, value, value, value}
}

func LargeComposition[T any](value T) struct{ A, B, C, D any } {
	return quadruple(quadruple(quadruple(quadruple(quadruple(quadruple(value))))))
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

type sourceCell[T any] struct{ Value T }

func (c *sourceCell[T]) Reset() {
	var zero T
	c.Value = zero
}

type sourceOuter[T any] struct{ sourceCell[T] }

func ResetPromotedSource[T any](input T) T {
	value := sourceOuter[T]{sourceCell: sourceCell[T]{Value: input}}
	value.Reset()
	return value.Value
}

func KeepPromotedSource[T any](input T) T {
	value := sourceOuter[T]{sourceCell: sourceCell[T]{Value: input}}
	return value.Value
}
