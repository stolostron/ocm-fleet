package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func TestManagedResourceMaintenance_RecoversUndispatchedGenerationsAndDeletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := setupExtensionResourceService(t)
		ctx := context.Background()
		now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
		seedManagedPlusInventoryTypeForDelete(t, h.store, now)
		tx, err := h.store.BeginReadOnly(ctx)
		if err != nil {
			t.Fatal(err)
		}
		typeDef, err := tx.ExtensionResources().GetType(ctx, deletePreflightResourceType)
		tx.RollbackOnExit()
		if err != nil {
			t.Fatal(err)
		}
		create := &domain.CreateManagedResourceWorkflowSpec{Store: h.store, Now: func() time.Time { return now }}
		// Run only the commit activity: neither workflow dispatch nor its
		// checkpoint exists. Maintenance must discover the work from storage.
		created, err := create.PersistManagedResource().Run(ctx, domain.CreateManagedResourceInput{RequestID: "undispatched-create", ResourceType: typeDef.ResourceType(), Name: "widgets/recovery", TypeDef: typeDef, Spec: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		if created.Fulfillment.ObservedGeneration() >= created.Fulfillment.Generation() {
			t.Fatal("test requires an undispatched generation")
		}
		if err := h.maintenance.RunOnce(ctx, now); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		current, err := h.svc.Get(ctx, typeDef.ResourceType(), created.Resource.Name())
		if err != nil {
			t.Fatal(err)
		}
		if current.Fulfillment.ObservedGeneration() != current.Fulfillment.Generation() {
			t.Fatal("maintenance did not dispatch the committed Create")
		}
		update := &domain.UpdateManagedResourceWorkflowSpec{Store: h.store, Now: func() time.Time { return now }}
		input := domain.UpdateManagedResourceInput{ResourceType: typeDef.ResourceType(), Name: current.Resource.Name(), Etag: current.Etag(), Spec: json.RawMessage(`{"message":"new intent"}`), UpdateSpec: true}
		if _, err := update.MutateSpec().Run(ctx, input); err != nil {
			t.Fatal(err)
		}
		if _, err := update.MutateSpec().Run(ctx, input); !errors.Is(err, domain.ErrStaleGeneration) {
			t.Fatalf("committed update replay must fail its old etag: %v", err)
		}
		if err := h.maintenance.RunOnce(ctx, now); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		current, err = h.svc.Get(ctx, typeDef.ResourceType(), created.Resource.Name())
		if err != nil {
			t.Fatal(err)
		}
		if current.Intent.Version != 2 || current.Fulfillment.Generation() != current.Fulfillment.ObservedGeneration() {
			t.Fatal("update recovery lost intent or left the generation undispatched")
		}
		deletion := &domain.DeleteManagedResourceWorkflowSpec{Store: h.store}
		deleteInput := domain.DeleteManagedResourceInput{ResourceType: typeDef.ResourceType(), Name: current.Resource.Name(), ExpectedUID: current.Resource.UID(), Etag: current.Etag()}
		if _, err := deletion.MutateToDeleting().Run(ctx, deleteInput); err != nil {
			t.Fatal(err)
		}
		if _, err := deletion.MutateToDeleting().Run(ctx, deleteInput); !errors.Is(err, domain.ErrStaleGeneration) {
			t.Fatalf("committed Delete replay must fail its old etag: %v", err)
		}
		if err := h.maintenance.RunOnce(ctx, now); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if _, err := h.svc.Get(ctx, typeDef.ResourceType(), created.Resource.Name()); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("maintenance must recover cleanup as well as orchestration: %v", err)
		}
		if err := h.maintenance.RunOnce(ctx, now.Add(domain.ManagedResourceCreationRetention)); err != nil {
			t.Fatal(err)
		}
		tx, err = h.store.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.RollbackOnExit()
		if _, err := tx.ExtensionResources().GetCreation(ctx, "undispatched-create", typeDef.ResourceType().FullName(created.Resource.Name())); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("retired creation decision was not collected: %v", err)
		}
	})
}

func TestManagedResourceMaintenance_WakesCleanupAfterLostSignal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := setupExtensionResourceService(t)
		ctx := context.Background()
		now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
		seedManagedPlusInventoryTypeForDelete(t, h.store, now)
		tx, err := h.store.BeginReadOnly(ctx)
		if err != nil {
			t.Fatal(err)
		}
		typeDef, err := tx.ExtensionResources().GetType(ctx, deletePreflightResourceType)
		tx.RollbackOnExit()
		if err != nil {
			t.Fatal(err)
		}
		create := &domain.CreateManagedResourceWorkflowSpec{Store: h.store, Now: func() time.Time { return now }}
		created, err := create.PersistManagedResource().Run(ctx, domain.CreateManagedResourceInput{RequestID: "lost-cleanup-signal", ResourceType: typeDef.ResourceType(), Name: "widgets/lost-signal", TypeDef: typeDef, Spec: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		deletion := &domain.DeleteManagedResourceWorkflowSpec{Store: h.store}
		if _, err := deletion.MutateToDeleting().Run(ctx, domain.DeleteManagedResourceInput{ResourceType: typeDef.ResourceType(), Name: created.Resource.Name(), Etag: created.Etag()}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.cleanup.Start(ctx, domain.DeleteManagedResourceCleanupInput{ResourceType: typeDef.ResourceType(), Name: created.Resource.Name(), FulfillmentID: created.Fulfillment.ID()}); err != nil {
			t.Fatal(err)
		}
		// Ensure the real cleanup workflow is already waiting for its signal.
		synctest.Wait()
		defer func() {
			// Also release it when an assertion fails, so a regression reports
			// the missing recovery instead of a synctest goroutine deadlock.
			if _, err := h.svc.Get(ctx, typeDef.ResourceType(), created.Resource.Name()); err == nil {
				_ = h.signaler.SignalDeleteCleanupComplete(ctx, created.Fulfillment.ID(), domain.DeleteCleanupCompleteEvent{FulfillmentID: created.Fulfillment.ID()})
				synctest.Wait()
			}
		}()

		orch := domain.NewOrchestrationWorkflowSpec(h.store, nil, domain.StrategyFactory{Store: h.store}, nil)
		if _, err := orch.CleanupDeliveryData().Run(ctx, created.Fulfillment.ID()); err != nil {
			t.Fatal(err)
		}
		tx, err = h.store.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.RollbackOnExit()
		f, err := tx.Fulfillments().Get(ctx, created.Fulfillment.ID())
		if err != nil {
			t.Fatal(err)
		}
		// Reproduce the commit-before-signal boundary without retaining an
		// orchestration workflow that can rescue the missing notification.
		f.MergeReconciliationResult(domain.ReconciliationResult{FulfillmentID: f.ID(), State: domain.FulfillmentStateDeleting}, f.Generation())
		f.CompleteReconciliation(f.Generation(), now)
		if err := tx.Fulfillments().Update(ctx, f); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := h.maintenance.RunOnce(ctx, now); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if _, err := h.svc.Get(ctx, typeDef.ResourceType(), created.Resource.Name()); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("completed deletion stranded its existing cleanup listener: %v", err)
		}
	})
}
