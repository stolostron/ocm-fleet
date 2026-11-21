package optional_test

import (
	"testing"

	"github.com/stolostron/ocm-fleet/server/pkg/optional"
)

func TestZeroValueIsAbsent(t *testing.T) {
	var value optional.Optional[string]
	if got, ok := value.Get(); ok || got != "" {
		t.Fatalf("zero optional = (%q, %t), want (empty, false)", got, ok)
	}
}

func TestSomePreservesPresence(t *testing.T) {
	t.Run("nonzero value", func(t *testing.T) {
		if got, ok := optional.Some("name").Get(); !ok || got != "name" {
			t.Fatalf("present value = (%q, %t), want (name, true)", got, ok)
		}
	})
	t.Run("zero value", func(t *testing.T) {
		if got, ok := optional.Some(0).Get(); !ok || got != 0 {
			t.Fatalf("present zero = (%d, %t), want (0, true)", got, ok)
		}
	})
	t.Run("nil pointer", func(t *testing.T) {
		if got, ok := optional.Some[*int](nil).Get(); !ok || got != nil {
			t.Fatalf("present nil pointer = (%v, %t), want (nil, true)", got, ok)
		}
	})
	t.Run("nil interface", func(t *testing.T) {
		if got, ok := optional.Some[any](nil).Get(); !ok || got != nil {
			t.Fatalf("present nil interface = (%v, %t), want (nil, true)", got, ok)
		}
	})
	t.Run("absent nested optional", func(t *testing.T) {
		var inner optional.Optional[int]
		got, ok := optional.Some(inner).Get()
		if !ok {
			t.Fatal("outer optional must be present")
		}
		if _, ok := got.Get(); ok {
			t.Fatal("inner optional must remain absent")
		}
	})
}

func TestSomeCopiesValues(t *testing.T) {
	type record struct{ Number int }
	source := record{Number: 1}
	value := optional.Some(source)
	source.Number = 2
	first, ok := value.Get()
	if !ok || first.Number != 1 {
		t.Fatalf("stored value = (%+v, %t), want ({Number:1}, true)", first, ok)
	}
	first.Number = 3
	if second, ok := value.Get(); !ok || second.Number != 1 {
		t.Fatalf("stored value after modifying a copy = (%+v, %t), want ({Number:1}, true)", second, ok)
	}
	copied := value
	value = optional.Some(record{Number: 4})
	if got, ok := copied.Get(); !ok || got.Number != 1 {
		t.Fatalf("copied optional after replacement = (%+v, %t), want ({Number:1}, true)", got, ok)
	}
	if got, ok := value.Get(); !ok || got.Number != 4 {
		t.Fatalf("replaced optional = (%+v, %t), want ({Number:4}, true)", got, ok)
	}
	value = optional.Optional[record]{}
	if _, ok := value.Get(); ok {
		t.Fatal("zero assignment must restore absence")
	}
}

func TestSomeRetainsOrdinaryShallowCopies(t *testing.T) {
	source := []int{1}
	value := optional.Some(source)
	source[0] = 2
	got, ok := value.Get()
	if !ok || len(got) != 1 || got[0] != 2 {
		t.Fatalf("stored slice = (%v, %t), want ([2], true)", got, ok)
	}
	got[0] = 3
	if source[0] != 3 {
		t.Fatal("extraction must preserve the slice's ordinary sharing semantics")
	}
}
