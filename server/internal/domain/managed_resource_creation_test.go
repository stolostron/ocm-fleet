package domain_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func TestExtensionResource_CreationDecisions(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	caller := domain.FederatedIdentity{Subject: "alice", Issuer: "https://issuer.example"}
	r := domain.NewExtensionResource(domain.NewExtensionResourceUID(), "records.example.io/Record", "records/creation", "accepted", &caller, now,
		domain.WithManagedState("creation"))
	// Construction itself captures the attempt; there is no second operation
	// that a creation path can forget before saving the aggregate.
	if pending := r.Snapshot().PendingCreation; pending == nil || pending.RequestID != "accepted" || pending.UID != r.UID() {
		t.Fatalf("constructor did not capture creation attempt: %+v", pending)
	}
	etag := (domain.ExtensionResourceView{Resource: *r}).Etag()
	want := domain.ManagedResourceCreationSnapshot{RequestID: "accepted", ResourceType: r.ResourceType(), Name: r.Name(), Caller: &caller, UID: r.UID()}
	snap := r.Snapshot()
	if snap.PendingCreation == nil || !reflect.DeepEqual(*snap.PendingCreation, want) {
		t.Fatalf("pending creation: %+v, want %+v", snap.PendingCreation, want)
	}
	if !r.UpdatedAt().Equal(now) || r.Managed().CurrentVersion() != 0 {
		t.Fatal("creation bookkeeping must not change intent version or update time")
	}
	if got := (domain.ExtensionResourceView{Resource: *r}).Etag(); got != etag {
		t.Fatal("creation bookkeeping must not invalidate the resource's etag")
	}
	// Like pending intents, these are transaction-scoped writes, not history
	// that should be loaded and rewritten on the next aggregate save.
	loaded := domain.ExtensionResourceFromSnapshot(snap)
	if loaded.Snapshot().PendingCreation != nil {
		t.Fatal("rehydration must not replay creation decisions")
	}
	if r.Snapshot().PendingCreation == nil {
		t.Fatal("taking a snapshot consumed pending creation decisions")
	}
	// Mutating the persistence DTO must not mutate the captured identity.
	snap.PendingCreation.RequestID = "changed"
	snap.PendingCreation.Caller.Subject = "changed"
	if got := r.Snapshot().PendingCreation; !reflect.DeepEqual(*got, want) {
		t.Fatal("mutating a snapshot changed the constructor-captured attempt")
	}
}

func TestManagedResourceCreation_RoundTrip(t *testing.T) {
	for _, uid := range []domain.ExtensionResourceUID{{}, domain.NewExtensionResourceUID()} {
		for _, caller := range []*domain.FederatedIdentity{nil, {}, {Subject: "alice", Issuer: "https://issuer.example"}} {
			original := domain.ManagedResourceCreationFromSnapshot(domain.ManagedResourceCreationSnapshot{
				RequestID: "creation-nonce", ResourceType: "records.example.io/Record", Name: "records/creation", Caller: caller, UID: uid,
			})
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var got domain.ManagedResourceCreation
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got != original {
				t.Fatalf("creation decision changed during serialization: %s", data)
			}
		}
	}
}

func TestManagedResourceCreation_CallerIdentityIsCopied(t *testing.T) {
	caller := domain.FederatedIdentity{Subject: "alice", Issuer: "https://issuer.example"}
	original := caller
	r := domain.NewExtensionResource(domain.NewExtensionResourceUID(), "records.example.io/Record", "records/creation", "nonce", &caller, time.Unix(0, 0))
	caller.Subject = "changed"
	decision := domain.ManagedResourceCreationFromSnapshot(*r.Snapshot().PendingCreation)
	if !decision.MatchesScope(r.ResourceType(), r.Name(), &original) || decision.MatchesScope(r.ResourceType(), r.Name(), &caller) || decision.MatchesScope(r.ResourceType(), r.Name(), nil) {
		t.Fatal("mutating constructor input changed the nonce's caller binding")
	}
	decision.Caller().Subject = "changed-again"
	if !decision.MatchesScope(r.ResourceType(), r.Name(), &original) {
		t.Fatal("mutating a returned caller changed the stored decision")
	}
}
