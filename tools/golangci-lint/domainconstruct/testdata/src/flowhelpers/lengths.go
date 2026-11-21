package flowhelpers

type Cursor struct{ position int }

type indexList []int

func Indexes(size int) []int {
	indexes := make([]int, size)
	for index := range indexes {
		indexes[index] = index
	}
	return indexes
}

func (c *Cursor) Next(size int) []int {
	indexes := make([]int, size)
	for index := range indexes {
		indexes[index] = c.position + index
	}
	c.position += size
	return indexes
}

func Truncated(size int) []int {
	indexes := make([]int, size)
	indexes = indexes[:size/2]
	return indexes
}

func ChangedParameter(size int) []int {
	size /= 2
	indexes := make([]int, size)
	return indexes
}

func MixedLengths(size int, shorten bool) []int {
	if shorten {
		return make([]int, size/2)
	}
	return make([]int, size)
}

func EscapedHeader(size int) []int {
	indexes := make([]int, size)
	shrink(&indexes)
	return indexes
}

func CapturedHeader(size int) (indexes []int) {
	indexes = make([]int, size)
	defer func() { indexes = nil }()
	return
}

func ConditionalHeader(size int, allocate bool) []int {
	var indexes []int
	if allocate {
		indexes = make([]int, size)
	}
	return indexes
}

func EarlyHeaderReturn(size int, skip bool) (indexes []int) {
	if skip {
		return
	}
	indexes = make([]int, size)
	return
}

func MethodShortened(size int) []int {
	indexes := make(indexList, size)
	indexes.shorten()
	return indexes
}

func (indexes *indexList) shorten() { *indexes = nil }

func shrink(indexes *[]int) { *indexes = nil }
