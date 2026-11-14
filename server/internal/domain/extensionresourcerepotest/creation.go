package extensionresourcerepotest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func runCreationDecisionTests(t *testing.T, factory Factory) {
	ctx := context.Background()
	tx := factory(t)
	defer tx.RollbackOnExit()
	repo := tx.ExtensionResources()
	name := domain.ResourceType("record.example.io/Record").FullName("records/retention")
	if _, err := repo.GetCreation(ctx, "accepted", name); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing decision: %v", err)
	}
	uid := domain.NewExtensionResourceUID()
	accepted := domain.NewManagedResourceCreation("accepted", "accepted-fingerprint", uid, fixedTime)
	if err := repo.PutCreation(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetCreation(ctx, "rejected", name); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	rejected := domain.NewManagedResourceCreation("rejected", "rejected-fingerprint", domain.ExtensionResourceUID{}, fixedTime)
	if err := repo.PutCreation(ctx, rejected); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetCreation(ctx, "accepted", name)
	if err != nil || got.UID() != uid || got.Fingerprint() != accepted.Fingerprint() || got.Expired(fixedTime.Add(365*24*time.Hour)) {
		t.Fatalf("live creation must retain its identity: %+v, %v", got.Snapshot(), err)
	}
	got, err = repo.GetCreation(ctx, "rejected", name)
	if err != nil || !got.AlreadyExists() || got.Expired(fixedTime.Add(domain.ManagedResourceCreationRetention-time.Nanosecond)) || !got.Expired(fixedTime.Add(domain.ManagedResourceCreationRetention)) {
		t.Fatalf("rejection retention: %+v, %v", got.Snapshot(), err)
	}
	if count, err := repo.PurgeExpiredCreations(ctx, fixedTime.Add(domain.ManagedResourceCreationRetention), 1); err != nil || count != 1 {
		t.Fatalf("bounded expiry batch: %d, %v", count, err)
	}
	if _, err := repo.GetCreation(ctx, "rejected", name); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired rejection survived pruning: %v", err)
	}
	retainedUntil := fixedTime.Add(2 * domain.ManagedResourceCreationRetention)
	if err := repo.RetireCreation(ctx, uid, retainedUntil); err != nil {
		t.Fatal(err)
	}
	// Replayed cleanup must neither move the deadline nor retire a new UID.
	if err := repo.RetireCreation(ctx, uid, retainedUntil.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.RetireCreation(ctx, domain.NewExtensionResourceUID(), retainedUntil.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetCreation(ctx, "accepted", name)
	if err != nil || !got.RetainUntil().Equal(retainedUntil) {
		t.Fatalf("retirement must preserve the first deadline: %+v, %v", got.Snapshot(), err)
	}
	if count, err := repo.PurgeExpiredCreations(ctx, retainedUntil.Add(-time.Nanosecond), 10); err != nil || count != 0 {
		t.Fatalf("premature expiry: %d, %v", count, err)
	}
	if count, err := repo.PurgeExpiredCreations(ctx, retainedUntil, 10); err != nil || count != 1 {
		t.Fatalf("retired creation expiry: %d, %v", count, err)
	}
}

func runManagedResourceWorkTests(t *testing.T, factory Factory) {
	ctx := context.Background()
	tx := factory(t)
	defer tx.RollbackOnExit()
	repo := tx.ExtensionResources()
	typeDef := seedType(t, tx, "record.example.io/Record")
	want := make(map[domain.ExtensionResourceUID]domain.ManagedResourceWork)
	for _, tc := range []struct {
		name         string
		state        domain.FulfillmentState
		pauseReason  string
		observed     domain.Generation
		needsWork    bool
		cleanupReady bool
	}{
		{name: "create", state: domain.FulfillmentStateCreating, observed: 1, needsWork: true},
		{name: "update", state: domain.FulfillmentStateActive, observed: 1, needsWork: true},
		{name: "delete", state: domain.FulfillmentStateDeleting, observed: 1, needsWork: true},
		{name: "delete-complete", state: domain.FulfillmentStateDeleting, observed: 2, needsWork: true, cleanupReady: true},
		{name: "delete-paused", state: domain.FulfillmentStateDeleting, observed: 2, pauseReason: "credentials expired", needsWork: true},
		{name: "converged", state: domain.FulfillmentStateActive, observed: 2},
		{name: "paused", state: domain.FulfillmentStateActive, observed: 1, pauseReason: "credentials expired"},
		{name: "failed-new-intent", state: domain.FulfillmentStateFailed, observed: 1, needsWork: true},
		{name: "failed-terminal", state: domain.FulfillmentStateFailed, observed: 2},
	} {
		fID := domain.FulfillmentID("work-" + tc.name)
		f := domain.FulfillmentFromSnapshot(domain.FulfillmentSnapshot{
			ID: fID, State: tc.state, PauseReason: tc.pauseReason,
			Generation: 2, ObservedGeneration: tc.observed,
			CreatedAt: fixedTime, UpdatedAt: fixedTime,
		})
		if err := tx.Fulfillments().Create(ctx, f); err != nil {
			t.Fatal(err)
		}
		er := newER(typeDef.ResourceType(), domain.ResourceName("records/"+tc.name), fID)
		if err := repo.Create(ctx, er); err != nil {
			t.Fatal(err)
		}
		if tc.needsWork {
			want[er.UID()] = domain.ManagedResourceWork{
				UID: er.UID(), ResourceType: typeDef.ResourceType(), Name: er.Name(), FulfillmentID: fID,
				Deleting: tc.state == domain.FulfillmentStateDeleting, Paused: tc.pauseReason != "", CleanupReady: tc.cleanupReady,
			}
		}
	}
	// Small pages exercise recovery beyond paused and completed deletions;
	// neither may hold the cursor in place and starve other generations.
	var cursor domain.ExtensionResourceUID
	for {
		page, err := repo.ListManagedResourceWork(ctx, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) > 2 {
			t.Fatalf("unbounded recovery query returned %d items", len(page))
		}
		for _, item := range page {
			if !cursor.IsZero() && item.UID.String() <= cursor.String() {
				t.Fatal("recovery cursor did not advance")
			}
			if expected, exists := want[item.UID]; !exists || item != expected {
				t.Fatalf("unexpected recovery work: %+v, expected %+v", item, expected)
			}
			delete(want, item.UID)
			cursor = item.UID
		}
		if len(page) < 2 {
			break
		}
	}
	if len(want) != 0 {
		t.Fatalf("recovery skipped %d resources", len(want))
	}
}
