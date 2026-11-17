package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

// ManagedResourceMaintenance recovers dispatch from current aggregate state.
// Eager mutation workflows provide the fast path; this pass also recovers failed
// workflows and commit/checkpoint gaps.
type ManagedResourceMaintenance struct {
	store         domain.Store
	orchestration domain.OrchestrationWorkflow
	cleanup       domain.DeleteManagedResourceCleanupWorkflow
	signaler      domain.DeleteCleanupSignaler
}

// NewManagedResourceMaintenance coordinates recovery through the existing
// orchestration and cleanup workflows; it owns no separate mutation journal.
func NewManagedResourceMaintenance(store domain.Store, orchestration domain.OrchestrationWorkflow, cleanup domain.DeleteManagedResourceCleanupWorkflow, signaler domain.DeleteCleanupSignaler) *ManagedResourceMaintenance {
	return &ManagedResourceMaintenance{store: store, orchestration: orchestration, cleanup: cleanup, signaler: signaler}
}

// RunOnce starts durable work without waiting for provisioning. Generation gaps
// remain discoverable until reconciled; unfinished deletions remain discoverable
// until removed. No receipt or successful original mutation replay is required.
// Each query is bounded, and a UID cursor prevents one failed resource from
// starving later pages.
func (s *ManagedResourceMaintenance) RunOnce(ctx context.Context) error {
	var after domain.ExtensionResourceUID
	var failures []error
	for {
		work, err := s.loadWork(ctx, after)
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		for _, item := range work {
			if err := ctx.Err(); err != nil {
				return errors.Join(append(failures, err)...)
			}
			if err := s.dispatch(ctx, item); err != nil {
				failures = append(failures, fmt.Errorf("recover %s: %w", item.ResourceType.FullName(item.Name), err))
			}
			after = item.UID
		}
		if len(work) < managedResourceMaintenancePageSize {
			return errors.Join(failures...)
		}
	}
}

const managedResourceMaintenancePageSize = 100

func (s *ManagedResourceMaintenance) loadWork(ctx context.Context, after domain.ExtensionResourceUID) ([]domain.ManagedResourceWork, error) {
	tx, err := s.store.BeginReadOnly(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.RollbackOnExit()
	work, err := tx.ExtensionResources().ListManagedResourceWork(ctx, after, managedResourceMaintenancePageSize)
	if err != nil {
		return nil, err
	}
	return work, tx.Commit()
}

func (s *ManagedResourceMaintenance) dispatch(ctx context.Context, item domain.ManagedResourceWork) error {
	if item.Deleting {
		// The cleanup listener must exist before orchestration can signal it.
		if _, err := s.cleanup.Start(ctx, domain.DeleteManagedResourceCleanupInput{ResourceType: item.ResourceType, Name: item.Name, FulfillmentID: item.FulfillmentID}); err != nil && !errors.Is(err, domain.ErrAlreadyRunning) {
			return err
		}
		if item.CleanupReady {
			// An existing listener may be waiting on a notification lost
			// after deletion reconciliation committed. Wake it from that
			// durable predicate without redelivering external deletion.
			return s.signaler.SignalDeleteCleanupComplete(ctx, item.FulfillmentID, domain.DeleteCleanupCompleteEvent{FulfillmentID: item.FulfillmentID})
		}
	}
	if item.Paused {
		return nil
	}
	_, err := s.orchestration.Start(ctx, item.FulfillmentID)
	if errors.Is(err, domain.ErrAlreadyRunning) {
		return nil
	}
	return err
}
