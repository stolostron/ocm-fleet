package domain

import "testing"

func TestRead(t *testing.T) {
	f := NewFulfillment()
	if f.generation != 1 { // want `access Fulfillment.generation through Fulfillment methods`
		t.Fatal("unexpected generation")
	}
}
