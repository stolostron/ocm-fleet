package domain_test

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
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

func TestCreateManagedResource_ReplayReadsCurrentIntent(t *testing.T) {
	store, _ := setupStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Orchestration: &fakeOrchestrationWorkflow{}, Now: func() time.Time { return now }}
	in := domain.CreateManagedResourceInput{RequestID: "current-create", ResourceType: typeDef.ResourceType(), Name: "records/current", TypeDef: typeDef, Spec: json.RawMessage(`{"values":{"message":"before"}}`)}
	initial, err := create.Run(&stubRecord{ctx: ctx}, in)
	if err != nil {
		t.Fatal(err)
	}
	update := &domain.UpdateManagedResourceWorkflowSpec{Store: store, Orchestration: &fakeOrchestrationWorkflow{}}
	changed, err := update.Run(&stubRecord{ctx: ctx}, domain.UpdateManagedResourceState{Input: domain.UpdateManagedResourceInput{ResourceType: in.ResourceType, Name: in.Name, Spec: json.RawMessage(`{"values":{"message":"after"}}`), UpdateSpec: true, Etag: initial.Etag()}})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := create.Run(&stubRecord{ctx: ctx}, in)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Resource.UID() != changed.Resource.UID() || replayed.Intent.Version != changed.Intent.Version || string(replayed.Intent.Spec) != string(changed.Intent.Spec) {
		t.Fatal("a creation decision must identify the current incarnation without retaining a historical resource response")
	}
}

func TestCreateManagedResource_ReplayIgnoresPayloadChanges(t *testing.T) {
	store, _ := setupStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Now: func() time.Time { return now }}
	in := domain.CreateManagedResourceInput{
		RequestID: "first-decision", ResourceType: typeDef.ResourceType(), Name: "records/first-decision", TypeDef: typeDef,
		Spec: json.RawMessage(`{"values":{"message":"original"}}`), Labels: map[string]string{"source": "original"},
		Provenance: &domain.Provenance{Sig: domain.Signature{SignatureBytes: []byte("original-proof")}},
		Auth:       domain.DeliveryAuth{Caller: &domain.SubjectClaims{FederatedIdentity: domain.FederatedIdentity{Subject: "alice", Issuer: "https://issuer.example"}}, Token: "original-token"},
	}
	first, err := create.PersistManagedResource().Run(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name  string
		apply func(*domain.CreateManagedResourceInput)
	}{
		{"spec", func(in *domain.CreateManagedResourceInput) {
			in.Spec = json.RawMessage(`{"values":{"message":"changed"}}`)
		}},
		{"labels", func(in *domain.CreateManagedResourceInput) { in.Labels = map[string]string{"source": "changed"} }},
		{"proof", func(in *domain.CreateManagedResourceInput) {
			in.Provenance = &domain.Provenance{Sig: domain.Signature{SignatureBytes: []byte("refreshed-proof")}}
		}},
		{"credentials", func(in *domain.CreateManagedResourceInput) { in.Auth.Token = "refreshed-token" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			retry := in
			change.apply(&retry)
			got, err := create.PersistManagedResource().Run(ctx, retry)
			if err != nil {
				t.Fatalf("a decided Create must return its incarnation despite changed retry payload: %v", err)
			}
			if got.Resource.UID() != first.Resource.UID() || got.Etag() != first.Etag() || got.Intent.Version != first.Intent.Version || !maps.Equal(got.Resource.Labels(), first.Resource.Labels()) || string(got.Intent.Spec) != string(first.Intent.Spec) {
				t.Fatal("a Create retry applied its changed payload")
			}
			if !reflect.DeepEqual(got.Fulfillment.Provenance(), first.Fulfillment.Provenance()) || got.Fulfillment.Auth().Token != first.Fulfillment.Auth().Token {
				t.Fatal("a Create retry replaced the accepted provenance or delivery credentials")
			}
		})
	}
	// The same rule applies to a rejected request, even after the occupant is
	// deleted: changed retry input cannot turn its rejection into a new Create.
	in.RequestID = "rejected-decision"
	if _, err := create.PersistManagedResource().Run(ctx, in); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("conflicting Create: %v", err)
	}
	cleanup := &domain.DeleteManagedResourceCleanupWorkflowSpec{Store: store}
	if _, err := cleanup.DeleteManagedResourceAndFulfillment().Run(ctx, domain.DeleteManagedResourceCleanupInput{ResourceType: in.ResourceType, Name: in.Name, FulfillmentID: first.Fulfillment.ID()}); err != nil {
		t.Fatal(err)
	}
	in.Spec = json.RawMessage(`{"values":{"message":"changed"}}`)
	if _, err := create.PersistManagedResource().Run(ctx, in); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("changed retry payload reopened a rejected Create: %v", err)
	}
	in.RequestID = "first-decision"
	if _, err := create.PersistManagedResource().Run(ctx, in); !errors.Is(err, domain.ErrCreationRetired) {
		t.Fatalf("changed retry payload reopened a retired incarnation: %v", err)
	}
}

func TestCreateManagedResource_ReplayChecksResourceAndCallerScope(t *testing.T) {
	store, _ := setupStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	// The application validates and resolves the requested type before entering
	// the workflow. Register the alternate scopes so these retries are valid
	// creation attempts that reach request-ID conflict handling.
	types := map[domain.ResourceType]domain.ExtensionResourceType{typeDef.ResourceType(): typeDef}
	tx, err := store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	for _, rt := range []domain.ResourceType{"other.example.io/Record", "records.example.io/Other"} {
		def := domain.NewExtensionResourceType(rt, "v1alpha", "records", now,
			domain.WithManagement(domain.NewRegisteredSelfTarget("record-local", "managed.record"), domain.Signature{}))
		if err := tx.ExtensionResources().CreateType(ctx, def); err != nil {
			t.Fatal(err)
		}
		types[rt] = def
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Now: func() time.Time { return now }}
	in := domain.CreateManagedResourceInput{
		RequestID: "scoped-acceptance", ResourceType: typeDef.ResourceType(), Name: "records/scoped", TypeDef: typeDef, Spec: json.RawMessage(`{}`),
		Auth: domain.DeliveryAuth{Caller: &domain.SubjectClaims{FederatedIdentity: domain.FederatedIdentity{Subject: "alice", Issuer: "https://issuer.example"}}},
	}
	if _, err := create.PersistManagedResource().Run(ctx, in); err != nil {
		t.Fatal(err)
	}
	in.RequestID = "scoped-rejection"
	if _, err := create.PersistManagedResource().Run(ctx, in); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatal(err)
	}
	for _, requestID := range []string{"scoped-acceptance", "scoped-rejection"} {
		for _, change := range []struct {
			name  string
			apply func(*domain.CreateManagedResourceInput)
		}{
			{"name", func(in *domain.CreateManagedResourceInput) { in.Name = "records/another" }},
			{"type", func(in *domain.CreateManagedResourceInput) { in.ResourceType = "other.example.io/Record" }},
			{"type-name", func(in *domain.CreateManagedResourceInput) { in.ResourceType = "records.example.io/Other" }},
			{"subject", func(in *domain.CreateManagedResourceInput) {
				in.Auth.Caller = &domain.SubjectClaims{FederatedIdentity: domain.FederatedIdentity{Subject: "bob", Issuer: "https://issuer.example"}}
			}},
			{"issuer", func(in *domain.CreateManagedResourceInput) {
				in.Auth.Caller = &domain.SubjectClaims{FederatedIdentity: domain.FederatedIdentity{Subject: "alice", Issuer: "https://other.example"}}
			}},
			{"anonymous", func(in *domain.CreateManagedResourceInput) { in.Auth.Caller = nil }},
		} {
			t.Run(requestID+"/"+change.name, func(t *testing.T) {
				retry := in
				retry.RequestID = requestID
				change.apply(&retry)
				retry.TypeDef = types[retry.ResourceType]
				if _, err := create.PersistManagedResource().Run(ctx, retry); !errors.Is(err, domain.ErrInvalidArgument) {
					t.Fatalf("request ID reused outside its original scope: %v", err)
				}
			})
		}
	}
}

func TestCreateManagedResource_DecisionsSurviveElapsedTime(t *testing.T) {
	store, _ := setupStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Orchestration: &fakeOrchestrationWorkflow{}, Now: func() time.Time { return now }}
	in := domain.CreateManagedResourceInput{RequestID: "occupied-create", ResourceType: typeDef.ResourceType(), Name: "records/replay-decision", TypeDef: typeDef, Spec: json.RawMessage(`{}`)}
	occupied, err := create.Run(&stubRecord{ctx: ctx}, in)
	if err != nil {
		t.Fatal(err)
	}
	in.RequestID = "rejected-create"
	if _, err := create.Run(&stubRecord{ctx: ctx}, in); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("conflicting create: %v", err)
	}
	cleanup := &domain.DeleteManagedResourceCleanupWorkflowSpec{Store: store}
	if _, err := cleanup.DeleteManagedResourceAndFulfillment().Run(ctx, domain.DeleteManagedResourceCleanupInput{ResourceType: in.ResourceType, Name: in.Name, FulfillmentID: occupied.Fulfillment.ID()}); err != nil {
		t.Fatal(err)
	}
	// Elapsed time must not turn an already decided Create into a new attempt.
	// Advance the injected clock, without waiting or exercising a collector.
	for _, elapsed := range []time.Duration{365 * 24 * time.Hour, 10 * 365 * 24 * time.Hour} {
		now = now.Add(elapsed)
		in.RequestID = "occupied-create"
		if _, err := create.Run(&stubRecord{ctx: ctx}, in); !errors.Is(err, domain.ErrCreationRetired) {
			t.Fatalf("accepted Create replay after deletion must remain retired: %v", err)
		}
		in.RequestID = "rejected-create"
		if _, err := create.Run(&stubRecord{ctx: ctx}, in); !errors.Is(err, domain.ErrAlreadyExists) {
			t.Fatalf("a rejected Create must remain rejected: %v", err)
		}
	}
	// The decision binds the request ID, not the reusable resource name.
	in.RequestID = "new-create"
	if _, err := create.Run(&stubRecord{ctx: ctx}, in); err != nil {
		t.Fatalf("a fresh request ID must allow a new incarnation: %v", err)
	}
}

func TestManagedResourceCleanup_RecoversWithoutRepeatingTheSignal(t *testing.T) {
	store, _ := setupStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Now: func() time.Time { return now }}
	created, err := create.PersistManagedResource().Run(ctx, domain.CreateManagedResourceInput{RequestID: "cleanup-recovery", ResourceType: typeDef.ResourceType(), Name: "records/cleanup-recovery", TypeDef: typeDef, Spec: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	deleteSpec := &domain.DeleteManagedResourceWorkflowSpec{Store: store}
	if _, err := deleteSpec.MutateToDeleting().Run(ctx, domain.DeleteManagedResourceInput{ResourceType: typeDef.ResourceType(), Name: created.Resource.Name(), Etag: created.Etag(), ExpectedUID: created.Resource.UID()}); err != nil {
		t.Fatal(err)
	}
	// No target has been provisioned. Finish the real delivery cleanup and
	// persist completion, with no cleanup workflow consuming the signal.
	orch := domain.NewOrchestrationWorkflowSpec(store, nil, domain.StrategyFactory{Store: store}, &stubRegistry{})
	if _, err := orch.CleanupDeliveryData().Run(ctx, created.Fulfillment.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := orch.PersistAndCompleteReconciliation().Run(ctx, domain.PersistAndCompleteInput{ReconciledGen: created.Fulfillment.Generation() + 1, Result: domain.ReconciliationResult{FulfillmentID: created.Fulfillment.ID(), State: domain.FulfillmentStateDeleting}}); err != nil {
		t.Fatal(err)
	}
	cleanup := &domain.DeleteManagedResourceCleanupWorkflowSpec{Store: store}
	// This record fails on Await, proving completed deletion is read from
	// durable state rather than requiring another signal or delivery pass.
	if _, err := cleanup.Run(&noSignalRecord{stubRecord: stubRecord{ctx: ctx}}, domain.DeleteManagedResourceCleanupInput{ResourceType: typeDef.ResourceType(), Name: created.Resource.Name(), FulfillmentID: created.Fulfillment.ID()}); err != nil {
		t.Fatal(err)
	}
	tx, err := store.BeginReadOnly(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	if _, err := tx.ExtensionResources().Get(ctx, typeDef.ResourceType().FullName(created.Resource.Name())); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("completed deletion was not removed: %v", err)
	}
}

type noSignalRecord struct{ stubRecord }

func (r *noSignalRecord) Await(string) (any, error) {
	return nil, errors.New("no cleanup signal is available")
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
	if _, err := create.Run(&stubRecord{ctx: ctx}, in); !errors.Is(err, domain.ErrCreationRetired) {
		t.Fatalf("replay of a retired incarnation must report absence: %v", err)
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
