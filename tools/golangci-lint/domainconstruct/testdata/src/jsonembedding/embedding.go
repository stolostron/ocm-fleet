package jsonembedding

type Diamond[Value, Direct any] struct {
	Left[Value, Direct]
	Right[Value, Direct]
}

type Left[Value, Direct any] struct{ Shared[Value, Direct] }
type Right[Value, Direct any] struct{ Shared[Value, Direct] }

type Shared[Value, Direct any] struct {
	Leaf[Value]
	Immediate Direct
}

type Leaf[T any] struct{ Value T }

type NamedTie[T any] struct {
	FirstNamed[T]
	SecondNamed[T]
}

type FirstNamed[T any] Shape[T]
type SecondNamed[T any] Shape[T]
type Shape[T any] struct{ Leaf[T] }
