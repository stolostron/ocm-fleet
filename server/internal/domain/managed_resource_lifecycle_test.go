package domain_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func TestCreateManagedResource_ReplayAfterCommit(t *testing.T) {
	store, _ := setupStore(t)
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	spec := &domain.CreateManagedResourceWorkflowSpec{Store: store, Orchestration: &fakeOrchestrationWorkflow{}, Now: func() time.Time { return now }}
	in := domain.CreateManagedResourceInput{
		ResourceType: typeDef.ResourceType(), Name: "records/replay", TypeDef: typeDef,
		Spec: json.RawMessage(`{"values":{"message":"hello"}}`),
	}
	first, err := spec.Run(&stubRecord{ctx: context.Background()}, in)
	if err != nil {
		t.Fatal(err)
	}
	// A worker can commit the activity transaction and crash before its
	// engine checkpoints the result. The replay must still reach dispatch.
	replayed, err := spec.Run(&stubRecord{ctx: context.Background()}, in)
	if err != nil {
		t.Fatalf("replaying a committed create must succeed: %v", err)
	}
	if replayed.Resource.UID() != first.Resource.UID() || replayed.Fulfillment.ID() != first.Fulfillment.ID() {
		t.Fatal("replay changed the resource incarnation")
	}
}

func TestManagedResource_ReplaysCannotChangeReplacement(t *testing.T) {
	store, _ := setupStore(t)
	ctx := context.Background()
	typeDef := seedLifecycleResourceType(t, store, time.Now())
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Orchestration: &fakeOrchestrationWorkflow{}}
	in := domain.CreateManagedResourceInput{
		RequestID: "original-create", ResourceType: typeDef.ResourceType(), Name: "records/incarnation", TypeDef: typeDef,
		Spec: json.RawMessage(`{"values":{"message":"original"}}`),
	}
	original, err := create.Run(&stubRecord{ctx: ctx}, in)
	if err != nil {
		t.Fatal(err)
	}
	// A second create that encountered the existing name must remain a
	// failed create even if it is retried after that incarnation disappears.
	losing := in
	losing.RequestID = "losing-create"
	if _, err := create.Run(&stubRecord{ctx: ctx}, losing); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("competing create: %v", err)
	}
	cleanup := &domain.DeleteManagedResourceCleanupWorkflowSpec{Store: store}
	cleanupIn := domain.DeleteManagedResourceCleanupInput{ResourceType: in.ResourceType, Name: in.Name, FulfillmentID: original.Fulfillment.ID()}
	if _, err := cleanup.DeleteManagedResourceAndFulfillment().Run(ctx, cleanupIn); err != nil {
		t.Fatal(err)
	}
	if _, err := create.Run(&stubRecord{ctx: ctx}, in); err != nil {
		t.Fatalf("successful request replay: %v", err)
	}
	if _, err := create.Run(&stubRecord{ctx: ctx}, losing); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("failed request replay: %v", err)
	}
	tx, err := store.BeginReadOnly(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.ExtensionResources().Get(ctx, in.ResourceType.FullName(in.Name))
	tx.RollbackOnExit()
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("create replay resurrected a deleted resource: %v", err)
	}
	in.RequestID = "replacement-create"
	in.Spec = json.RawMessage(`{"values":{"message":"replacement"}}`)
	replacement, err := create.Run(&stubRecord{ctx: ctx}, in)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Resource.UID() == original.Resource.UID() {
		t.Fatal("replacement reused its predecessor's UID")
	}
	if _, err := cleanup.DeleteManagedResourceAndFulfillment().Run(ctx, cleanupIn); err != nil {
		t.Fatal(err)
	}
	tx, err = store.BeginReadOnly(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	got, err := tx.ExtensionResources().GetView(ctx, in.ResourceType.FullName(in.Name))
	if err != nil {
		t.Fatalf("old cleanup removed the replacement: %v", err)
	}
	if got.Resource.UID() != replacement.Resource.UID() || got.Intent.Version != 1 {
		t.Fatal("old cleanup changed the replacement")
	}
}

func seedLifecycleResourceType(t *testing.T, store domain.Store, now time.Time) domain.ExtensionResourceType {
	t.Helper()
	typeDef := domain.NewExtensionResourceType("records.example.io/Record", "v1alpha", "records", now,
		domain.WithManagement(domain.NewRegisteredSelfTarget("record-local", "managed.record"), domain.Signature{}))
	tx, err := store.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	if err := tx.ExtensionResources().CreateType(context.Background(), typeDef); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return typeDef
}

func TestFulfillment_ReconcilingAfterSpecUpdate(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	f := domain.NewFulfillment("updated", domain.DeliveryAuth{}, nil, nil, now)
	f.AdvanceManifestStrategy(domain.ManifestStrategySpec{Type: domain.ManifestStrategyInline}, now)
	f.ApplyReconciliationResult(domain.ReconciliationResult{State: domain.FulfillmentStateActive})
	f.CompleteReconciliation(f.Generation(), now)
	// Reading back establishes the generation baseline for the next mutation.
	f = domain.FulfillmentFromSnapshot(f.Snapshot())
	f.AdvanceManifestStrategy(domain.ManifestStrategySpec{Type: domain.ManifestStrategyInline}, now.Add(time.Minute))
	if !f.Reconciling() {
		t.Fatal("an active fulfillment with an unobserved spec generation must be reconciling")
	}
}

func TestPersistReconciliation_StaleResultPreservesDeletionAndCredentials(t *testing.T) {
	store, _ := setupStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	f := domain.NewFulfillment("stale-result", domain.DeliveryAuth{Token: "create-token"}, nil, nil, now)
	f.AdvanceManifestStrategy(domain.ManifestStrategySpec{Type: domain.ManifestStrategyInline}, now)
	f.AdvancePlacementStrategy(domain.PlacementStrategySpec{Type: domain.PlacementStrategyAll}, now)
	tx, err := store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	if err := tx.Fulfillments().Create(ctx, f); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	tx, err = store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	f, err = tx.Fulfillments().Get(ctx, f.ID())
	if err != nil {
		t.Fatal(err)
	}
	f.TransitionToDeleting(domain.DeliveryAuth{Token: "delete-token"})
	if err := tx.Fulfillments().Update(ctx, f); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	spec := domain.NewOrchestrationWorkflowSpec(store, nil, domain.StrategyFactory{Store: store}, nil)
	restart, err := spec.PersistAndCompleteReconciliation().Run(ctx, domain.PersistAndCompleteInput{
		ReconciledGen: 1,
		Result: domain.ReconciliationResult{
			FulfillmentID:   f.ID(),
			State:           domain.FulfillmentStateActive,
			ResolvedTargets: []domain.TargetID{"delivered-before-delete"},
			Auth:            domain.DeliveryAuth{Token: "create-token"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !restart {
		t.Fatal("a newer generation must restart reconciliation")
	}
	tx, err = store.BeginReadOnly(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	f, err = tx.Fulfillments().Get(ctx, f.ID())
	if err != nil {
		t.Fatal(err)
	}
	if f.State() != domain.FulfillmentStateDeleting || f.Auth().Token != "delete-token" {
		t.Fatalf("stale reconciliation overwrote newer deletion: state=%s, credentials preserved=%t", f.State(), f.Auth().Token == "delete-token")
	}
	if len(f.ResolvedTargets()) != 1 || f.ResolvedTargets()[0] != "delivered-before-delete" {
		t.Fatal("stale result lost the delivery history needed to remove the resource")
	}
}
