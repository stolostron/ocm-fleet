package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DeleteManagedResourceCleanupWorkflowSpec is a long-running
// background workflow that checks for completed deletion reconciliation or
// waits for its notification, then atomically hard-deletes the managed resource
// row and fulfillment row.
//
// Pass this spec to [Registry.RegisterDeleteManagedResourceCleanup] to
// obtain a [DeleteManagedResourceCleanupWorkflow] that can start
// instances.
type DeleteManagedResourceCleanupWorkflowSpec struct {
	Store    Store
	Observer DeleteObserver
	Now      func() time.Time
}

func (s *DeleteManagedResourceCleanupWorkflowSpec) deleteObserver() DeleteObserver {
	if s.Observer != nil {
		return s.Observer
	}
	return NoOpDeleteObserver{}
}

func (s *DeleteManagedResourceCleanupWorkflowSpec) Name() string {
	return "delete-managed-resource-cleanup"
}

// CleanupRequired avoids leaving a new listener waiting forever when a recovery
// sweep races completion of a previous cleanup workflow for this fulfillment.
func (s *DeleteManagedResourceCleanupWorkflowSpec) CleanupRequired() Activity[FulfillmentID, managedResourceCleanupState] {
	return NewActivity("mr-cleanup-required", func(ctx context.Context, id FulfillmentID) (managedResourceCleanupState, error) {
		tx, err := s.Store.BeginReadOnly(ctx)
		if err != nil {
			return managedResourceCleanupState{}, err
		}
		defer tx.RollbackOnExit()
		f, err := tx.Fulfillments().Get(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return managedResourceCleanupState{}, nil
		}
		if err != nil {
			return managedResourceCleanupState{}, err
		}
		return managedResourceCleanupState{Exists: true, Ready: f.DeletionReconciled()}, tx.Commit()
	})
}

type managedResourceCleanupState struct {
	Exists bool
	Ready  bool
}

// DeleteManagedResourceAndFulfillment removes the managed resource row,
// all versioned resource intents for that resource, and the fulfillment
// row in a single transaction after orchestration has finished
// delivery-side cleanup. The managed-resource and fulfillment deletes
// tolerate [ErrNotFound] so the activity is idempotent on replay.
func (s *DeleteManagedResourceCleanupWorkflowSpec) DeleteManagedResourceAndFulfillment() Activity[DeleteManagedResourceCleanupInput, struct{}] {
	return NewActivity("delete-managed-resource-and-fulfillment", func(ctx context.Context, input DeleteManagedResourceCleanupInput) (struct{}, error) {
		tx, err := s.Store.Begin(ctx)
		if err != nil {
			return struct{}{}, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.RollbackOnExit()

		fullName := input.ResourceType.FullName(input.Name)
		er, err := tx.ExtensionResources().Get(ctx, fullName)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return struct{}{}, err
		}
		// A delayed cleanup belongs to its fulfillment, never to a later
		// resource that happens to reuse the same human-readable name.
		if err == nil && er.Managed() != nil && er.Managed().FulfillmentID() == input.FulfillmentID {
			now := time.Now()
			if s.Now != nil {
				now = s.Now()
			}
			if err := tx.ExtensionResources().RetireCreation(ctx, er.UID(), now.Add(ManagedResourceCreationRetention)); err != nil {
				return struct{}{}, fmt.Errorf("retire creation: %w", err)
			}
			if err := tx.ExtensionResources().Delete(ctx, fullName); err != nil && !errors.Is(err, ErrNotFound) {
				return struct{}{}, fmt.Errorf("delete extension resource row: %w", err)
			}
		}
		if err := tx.Fulfillments().Delete(ctx, input.FulfillmentID); err != nil && !errors.Is(err, ErrNotFound) {
			return struct{}{}, fmt.Errorf("delete fulfillment row: %w", err)
		}

		return struct{}{}, tx.Commit()
	})
}

// Run is the workflow body: establish deletion reconciliation is complete,
// using persisted state or its notification, then delete the managed resource
// and fulfillment rows.
func (s *DeleteManagedResourceCleanupWorkflowSpec) Run(record Record, input DeleteManagedResourceCleanupInput) (struct{}, error) {
	_, probe := s.deleteObserver().ManagedResourceCleanupStarted(record.Context(), input)
	defer probe.End()
	required, err := RunActivity(record, s.CleanupRequired(), input.FulfillmentID)
	if err != nil {
		probe.Error(err)
		return struct{}{}, err
	}
	if !required.Exists {
		return struct{}{}, nil
	}

	// A successful deletion reconciliation is durable before its signal is
	// sent. Recovery can therefore finish even if that signal was lost with
	// an earlier workflow execution; it need not redeliver external deletion.
	if !required.Ready {
		if _, err := AwaitSignal(record, DeleteCleanupCompleteSignal); err != nil {
			probe.Error(err)
			return struct{}{}, fmt.Errorf("await delete-cleanup-complete: %w", err)
		}
		probe.SignalReceived()
	}

	if _, err := RunActivity(record, s.DeleteManagedResourceAndFulfillment(), input); err != nil {
		probe.Error(err)
		return struct{}{}, fmt.Errorf("delete managed resource and fulfillment: %w", err)
	}
	probe.RowsDeleted()

	return struct{}{}, nil
}
