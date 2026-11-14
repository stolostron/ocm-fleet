package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func TestManagedResourceCreation_RoundTrip(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	for _, uid := range []domain.ExtensionResourceUID{{}, domain.NewExtensionResourceUID()} {
		original := domain.NewManagedResourceCreation("creation-nonce", "request-fingerprint", uid, now)
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var got domain.ManagedResourceCreation
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if got.Snapshot() != original.Snapshot() {
			t.Fatalf("creation decision changed during serialization: %s", data)
		}
	}
}
