package domain

type Fulfillment struct {
	id         string
	generation int
	internal   int
}

func (f *Fulfillment) ID() string      { return f.id }
func (f *Fulfillment) Generation() int { return f.generation }
func (f *Fulfillment) Advance()        { f.generation++ }

// Closures inside methods retain the method's access to its own type.
func (f *Fulfillment) Reader() func() int { return func() int { return f.generation } }

func NewFulfillment() *Fulfillment {
	f := &Fulfillment{}
	f.generation = 1
	return f
}

func Read(f *Fulfillment) int {
	_ = f.ID()
	_ = f.internal      // No public getter: deliberately outside the initial rule.
	_ = f.id            // want `access Fulfillment.id through Fulfillment methods; use ID\(\) for reads`
	return f.generation // want `access Fulfillment.generation through Fulfillment methods; use Generation\(\) for reads`
}

func Write(f *Fulfillment) {
	f.generation++ // want `access Fulfillment.generation through Fulfillment methods`
}

type Other struct{}

func (*Other) Read(f *Fulfillment) int {
	return f.generation // want `access Fulfillment.generation through Fulfillment methods`
}

type Alias = Fulfillment

func ReadAlias(f *Alias) int {
	return f.generation // want `access Fulfillment.generation through Fulfillment methods`
}

type Embedded struct{ *Fulfillment }

func ReadEmbedded(f *Embedded) int {
	return f.generation // want `access Fulfillment.generation through Fulfillment methods`
}

type RecordSnapshot struct{ Generation int }

func ReadSnapshot(s RecordSnapshot) int { return s.Generation }

type helper struct{ generation int }

func ReadHelper(h helper) int { return h.generation }

func NewOther(f *Fulfillment) *Other {
	_ = f.generation // want `access Fulfillment.generation through Fulfillment methods`
	return &Other{}
}

type Value[T any] struct{ value T }

func (v *Value[T]) Value() T    { return v.value }
func (v *Value[T]) Set(value T) { v.value = value }

func ReadValue(v *Value[string]) string {
	return v.value // want `access Value.value through Value methods`
}
