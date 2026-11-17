package domain_test

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
	"github.com/stolostron/ocm-fleet/server/internal/infrastructure/sqlite"
)

func TestCreateManagedResource_ConflictsLeaveNoDerivedState(t *testing.T) {
	store, _ := setupStore(t)
	db := store.(*sqlite.Store).DB
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Now: func() time.Time { return now }}
	input := domain.CreateManagedResourceInput{
		RequestID: "accepted", ResourceType: typeDef.ResourceType(), Name: "records/creation-state", TypeDef: typeDef, Spec: []byte(`{}`),
	}
	countState := func() map[string]int {
		t.Helper()
		counts := map[string]int{}
		for _, table := range []string{"extension_resources", "extension_resource_managed", "resource_intents", "fulfillments", "manifest_strategies", "placement_strategies", "rollout_strategies"} {
			var count int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
				t.Fatal(err)
			}
			counts[table] = count
		}
		return counts
	}
	assertDecisions := func(want int) {
		t.Helper()
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM managed_resource_creations").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("retained decisions: %d, want %d", count, want)
		}
	}
	original, err := create.PersistManagedResource().Run(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	baseline := countState()
	if baseline["fulfillments"] != 1 || baseline["extension_resources"] != 1 {
		t.Fatalf("initial creation did not persist its resource and fulfillment: %v", baseline)
	}
	// Neither a new rejected attempt nor a replay may commit the fresh
	// fulfillment and strategy versions constructed for that invocation.
	input.RequestID = "rejected"
	if _, err := create.PersistManagedResource().Run(ctx, input); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("conflicting Create: %v", err)
	}
	input.RequestID = "accepted"
	if _, err := create.PersistManagedResource().Run(ctx, input); err != nil {
		t.Fatal(err)
	}
	if got := countState(); !maps.Equal(got, baseline) {
		t.Fatalf("conflict or replay left derived state: %v, want %v", got, baseline)
	}
	assertDecisions(2)

	cleanup := &domain.DeleteManagedResourceCleanupWorkflowSpec{Store: store}
	if _, err := cleanup.DeleteManagedResourceAndFulfillment().Run(ctx, domain.DeleteManagedResourceCleanupInput{
		ResourceType: input.ResourceType, Name: input.Name, FulfillmentID: original.Fulfillment.ID(),
	}); err != nil {
		t.Fatal(err)
	}
	for _, nonce := range []string{"accepted", "rejected"} {
		input.RequestID = nonce
		_, err := create.PersistManagedResource().Run(ctx, input)
		want := domain.ErrAlreadyExists
		if nonce == "accepted" {
			want = domain.ErrCreationRetired
		}
		if !errors.Is(err, want) {
			t.Fatalf("retired-name replay %q: %v, want %v", nonce, err, want)
		}
	}
	for table, count := range countState() {
		if count != 0 {
			t.Fatalf("retired-name replay left %d rows in %s", count, table)
		}
	}
	assertDecisions(2)
}

func TestCreateManagedResource_FulfillmentFailureRollsBackAcceptance(t *testing.T) {
	store, _ := setupStore(t)
	db := store.(*sqlite.Store).DB
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	// Fail the real fulfillment insert after the resource and acceptance have
	// been written. The transaction must discard both, allowing the same nonce
	// to succeed once persistence recovers.
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER fail_fulfillment BEFORE INSERT ON fulfillments
BEGIN SELECT RAISE(ABORT, 'injected fulfillment failure'); END`); err != nil {
		t.Fatal(err)
	}
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Now: func() time.Time { return now }}
	input := domain.CreateManagedResourceInput{
		RequestID: "retry-after-storage-failure", ResourceType: typeDef.ResourceType(), Name: "records/storage-failure", TypeDef: typeDef, Spec: []byte(`{}`),
	}
	if _, err := create.PersistManagedResource().Run(ctx, input); err == nil || domain.IsTerminal(err) {
		t.Fatalf("storage failure must be retryable: %v", err)
	}
	for _, table := range []string{"extension_resources", "extension_resource_managed", "resource_intents", "managed_resource_creations", "fulfillments"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("rolled-back acceptance left %d rows in %s", count, table)
		}
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER fail_fulfillment"); err != nil {
		t.Fatal(err)
	}
	if _, err := create.PersistManagedResource().Run(ctx, input); err != nil {
		t.Fatalf("same nonce could not recover after rollback: %v", err)
	}
}

func TestCreateManagedResource_InternalUniquenessFailureIsNotNameConflict(t *testing.T) {
	store, _ := setupStore(t)
	db := store.(*sqlite.Store).DB
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Now: func() time.Time { return now }}
	input := domain.CreateManagedResourceInput{
		RequestID: "original", ResourceType: typeDef.ResourceType(), Name: "records/original", TypeDef: typeDef, Spec: []byte(`{}`),
	}
	if _, err := create.PersistManagedResource().Run(ctx, input); err != nil {
		t.Fatal(err)
	}
	// Introduce a real internal uniqueness failure, independent of the resource
	// name or request ID. Reporting AlreadyExists would falsely tell the client
	// that a durable rejection exists and that it can stop resolving this nonce.
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX fail_intent ON resource_intents(spec)`); err != nil {
		t.Fatal(err)
	}
	input.RequestID, input.Name = "retry-internal-failure", "records/another"
	if _, err := create.PersistManagedResource().Run(ctx, input); err == nil || domain.IsTerminal(err) || errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("internal uniqueness failure must remain a retryable storage error: %v", err)
	}
	tx, err := store.BeginReadOnly(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	if _, err := tx.ExtensionResources().GetCreation(ctx, input.RequestID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("failed write left a decision: %v", err)
	}
	tx.RollbackOnExit()
	if _, err := db.ExecContext(ctx, `DROP INDEX fail_intent`); err != nil {
		t.Fatal(err)
	}
	if _, err := create.PersistManagedResource().Run(ctx, input); err != nil {
		t.Fatalf("same nonce could not recover after internal failure: %v", err)
	}
}
