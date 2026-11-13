package values

import "testing"

func TestUncheckedFixture(t *testing.T) {
	_ = ID("test") // want "construct values.ID through NewID or RestoreID"
}
