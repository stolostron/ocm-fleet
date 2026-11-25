package extensionresourcerepotest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func runCreationDecisionTests(t *testing.T, factory Factory) {
	t.Run("InsertFirstConflicts", func(t *testing.T) { runInsertFirstConflictTest(t, factory) })
	t.Run("anonymous", func(t *testing.T) { runCreationDecisionTest(t, factory, nil) })
	t.Run("authenticated", func(t *testing.T) {
		runCreationDecisionTest(t, factory, &domain.FederatedIdentity{Subject: "alice", Issuer: "https://issuer.example"})
	})
	t.Run("empty-identity", func(t *testing.T) { runCreationDecisionTest(t, factory, &domain.FederatedIdentity{}) })
}

func runInsertFirstConflictTest(t *testing.T, factory Factory) {
	t.Helper()
	ctx := context.Background()
	tx := factory(t)
	defer tx.RollbackOnExit()
	repo := tx.ExtensionResources()
	rt := domain.ResourceType("record.example.io/Record")
	seedType(t, tx, rt)
	name := domain.ResourceName("records/insert-first")
	original := domain.NewExtensionResource(domain.NewExtensionResourceUID(), rt, name, "accepted", nil, fixedTime)
	if result, err := repo.Create(ctx, original); err != nil || result != domain.ExtensionResourceCreated {
		t.Fatalf("new acceptance: %v, %v", result, err)
	}
	// A name conflict must record its own nonce without loading or changing
	// the occupying aggregate. This is a successful rejection-only save.
	rejected := domain.NewExtensionResource(domain.NewExtensionResourceUID(), rt, name, "rejected", nil, fixedTime)
	if result, err := repo.Create(ctx, rejected); err != nil || result != domain.ExtensionResourceCreateRejected {
		t.Fatalf("name conflict must save its creation decision: %v, %v", result, err)
	}
	decision, err := repo.GetCreation(ctx, "rejected")
	if err != nil || !decision.AlreadyExists() {
		t.Fatalf("name conflict was not recorded: %+v, %v", decision.Snapshot(), err)
	}
	if err := repo.Delete(ctx, rt.FullName(name)); err != nil {
		t.Fatal(err)
	}
	// The free name is insufficient to accept either old nonce. A candidate
	// insert must be discarded when its immutable decision already exists.
	for _, nonce := range []string{"accepted", "rejected"} {
		for _, candidateName := range []domain.ResourceName{name, "records/another"} {
			candidate := domain.NewExtensionResource(domain.NewExtensionResourceUID(), rt, candidateName, nonce, nil, fixedTime)
			if result, err := repo.Create(ctx, candidate); err != nil || result != domain.ExtensionResourceCreateAlreadyDecided {
				t.Fatalf("recorded nonce %q: %v, %v", nonce, result, err)
			}
			if _, err := repo.Get(ctx, rt.FullName(candidateName)); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("recorded nonce %q left a provisional resource: %v", nonce, err)
			}
		}
	}
}

func runCreationDecisionTest(t *testing.T, factory Factory, caller *domain.FederatedIdentity) {
	t.Helper()
	ctx := context.Background()
	tx := factory(t)
	defer tx.RollbackOnExit()
	repo := tx.ExtensionResources()
	rt := domain.ResourceType("record.example.io/Record")
	seedType(t, tx, rt)
	seedFulfillment(t, tx, "creation-decisions", fixedTime)
	name := rt.FullName("records/decisions")
	if _, err := repo.GetCreation(ctx, "accepted"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing decision: %v", err)
	}
	er := domain.NewExtensionResource(domain.NewExtensionResourceUID(), rt, name.ResourceName(), "accepted", caller, fixedTime,
		domain.WithManagedState("creation-decisions"))
	if _, err := er.RecordIntent([]byte(`{}`), fixedTime); err != nil {
		t.Fatal(err)
	}
	if result, err := repo.Create(ctx, er); err != nil || result != domain.ExtensionResourceCreated {
		t.Fatalf("new acceptance: %v, %v", result, err)
	}
	if er.Snapshot().PendingCreation != nil {
		t.Fatal("a successful save must consume its pending creation")
	}
	accepted := domain.ManagedResourceCreationSnapshot{RequestID: "accepted", ResourceType: rt, Name: er.Name(), Caller: caller, UID: er.UID()}
	got, err := repo.GetCreation(ctx, "accepted")
	if err != nil || !reflect.DeepEqual(got.Snapshot(), accepted) {
		t.Fatalf("Create must persist its accepted creation decision: %+v, %v", got.Snapshot(), err)
	}
	er, err = repo.Get(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	before := er.Snapshot()
	// The occupying resource can have a different type from the rejected
	// attempt. Its own type must not replace the attempt's resource scope.
	attemptedType := domain.ResourceType("record.example.io/Other")
	seedType(t, tx, attemptedType)
	attempt := domain.NewExtensionResource(domain.NewExtensionResourceUID(), attemptedType, er.Name(), "rejected", caller, fixedTime,
		domain.WithManagedState("must-not-be-saved"), domain.WithExtensionLabels(map[string]string{"attempt": "rejected"}))
	if _, err := attempt.RecordIntent([]byte(`{"rejected":true}`), fixedTime); err != nil {
		t.Fatal(err)
	}
	if result, err := repo.Create(ctx, attempt); err != nil || result != domain.ExtensionResourceCreateRejected {
		t.Fatalf("name conflict: %v, %v", result, err)
	}
	rejected := domain.ManagedResourceCreationSnapshot{RequestID: "rejected", ResourceType: attemptedType, Name: er.Name(), Caller: caller}
	got, err = repo.GetCreation(ctx, "rejected")
	if err != nil || !got.AlreadyExists() || !reflect.DeepEqual(got.Snapshot(), rejected) {
		t.Fatalf("Create must persist its rejected creation decision: %+v, %v", got.Snapshot(), err)
	}
	if _, err := repo.GetIntent(ctx, attempt.UID(), 1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a rejection saved the attempted intent: %v", err)
	}
	er, err = repo.Get(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(er.Snapshot(), before) {
		t.Fatal("recording a rejected Create changed the occupying aggregate")
	}
	// A later ordinary save must not rewrite old decisions or require loading
	// the resource's growing history of rejected attempts.
	if err := repo.Update(ctx, er); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, name); err != nil {
		t.Fatal(err)
	}
	replacement := newER(rt, name.ResourceName(), "creation-decisions")
	if result, err := repo.Create(ctx, replacement); err != nil || result != domain.ExtensionResourceCreated {
		t.Fatalf("name reuse with a fresh nonce: %v, %v", result, err)
	}
	for _, want := range []domain.ManagedResourceCreationSnapshot{accepted, rejected} {
		got, err := repo.GetCreation(ctx, want.RequestID)
		if err != nil || !reflect.DeepEqual(got.Snapshot(), want) {
			t.Fatalf("deletion and name reuse must preserve the creation decision: %+v, %v", got.Snapshot(), err)
		}
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
		if _, err := repo.Create(ctx, er); err != nil {
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
