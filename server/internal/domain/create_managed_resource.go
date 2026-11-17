package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CreateManagedResourceInput carries all the fields needed to create a
// managed resource instance. The application service pre-validates the
// spec against the registered protobuf schema before starting this workflow.
type CreateManagedResourceInput struct {
	RequestID    string
	ResourceType ResourceType
	Name         ResourceName
	Spec         json.RawMessage
	Labels       map[string]string
	TypeDef      ExtensionResourceType
	Provenance   *Provenance
	Auth         DeliveryAuth
}

// CreateManagedResourceWorkflowSpec persists a managed resource
// (HEAD + intent v1 + derived fulfillment) and starts orchestration.
// Follows the same structural pattern as [CreateDeploymentWorkflowSpec].
type CreateManagedResourceWorkflowSpec struct {
	Store         Store
	Orchestration OrchestrationWorkflow
	Now           func() time.Time
}

func (s *CreateManagedResourceWorkflowSpec) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *CreateManagedResourceWorkflowSpec) Name() string { return "create-managed-resource" }

// PersistManagedResource creates the extension resource aggregate (with
// managed state, initial intent, and derived fulfillment) in a single
// transaction.
func (s *CreateManagedResourceWorkflowSpec) PersistManagedResource() Activity[CreateManagedResourceInput, ExtensionResourceView] {
	return NewActivity("persist-managed-resource", func(ctx context.Context, in CreateManagedResourceInput) (ExtensionResourceView, error) {
		tx, err := s.Store.Begin(ctx)
		if err != nil {
			return ExtensionResourceView{}, fmt.Errorf("begin tx: %w", err)
		}
		defer tx.RollbackOnExit()

		mgmt := in.TypeDef.Management()
		if mgmt == nil {
			return ExtensionResourceView{}, fmt.Errorf(
				"%w: type %q has no management metadata",
				ErrInvalidArgument, in.ResourceType)
		}

		fID := FulfillmentID(uuid.New().String())

		// No platform-identity claim is needed here: a platform
		// resource has no UID of its own and no eager row to create --
		// its representation is derived on read, by name, once the
		// extension resource row below exists.

		now := s.now()
		// Create the extension resource with managed state.
		er := NewExtensionResource(NewExtensionResourceUID(), in.ResourceType, in.Name, in.RequestID, mutationActor(in.Auth), now,
			WithManagedState(fID),
			WithExtensionLabels(in.Labels),
		)
		intent, err := er.RecordIntent(in.Spec, now)
		if err != nil {
			return ExtensionResourceView{}, fmt.Errorf("record intent: %w", err)
		}

		// Insert first. Unique resource names and nonces arbitrate concurrent
		// attempts without preliminary reads or advisory locks. No fulfillment
		// is written until the repository reports a new acceptance.
		result, err := tx.ExtensionResources().Create(ctx, er)
		if err != nil {
			return ExtensionResourceView{}, fmt.Errorf("create extension resource: %w", err)
		}
		if result == ExtensionResourceCreateAlreadyDecided {
			return replayManagedResourceCreation(ctx, tx, in)
		}
		if result == ExtensionResourceCreateRejected {
			// Remember even an AlreadyExists response. A delayed retry of this
			// same request must not recreate the name after its owner deletes it.
			if err := tx.Commit(); err != nil {
				return ExtensionResourceView{}, err
			}
			return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: this Create attempt was rejected", ErrAlreadyExists))
		}

		ms, ps, rs := mgmt.Relation().DeriveStrategies(intent)

		var attestRef *AttestationRef
		if in.Provenance != nil {
			rt := in.ResourceType
			attestRef = &AttestationRef{RelationRef: &rt}
		}

		f := NewFulfillment(fID, in.Auth, in.Provenance, attestRef, now)
		f.AdvanceManifestStrategy(ms, now)
		f.AdvancePlacementStrategy(ps, now)
		f.AdvanceRolloutStrategy(rs, now)

		if err := tx.Fulfillments().Create(ctx, f); err != nil {
			if errors.Is(err, ErrAlreadyExists) {
				return ExtensionResourceView{}, fmt.Errorf("%w: fulfillment identity collision", ErrStaleGeneration)
			}
			return ExtensionResourceView{}, fmt.Errorf("create fulfillment: %w", err)
		}

		view := ExtensionResourceView{Resource: *er, Intent: &intent, Fulfillment: f}
		if err := tx.Commit(); err != nil {
			return ExtensionResourceView{}, fmt.Errorf("commit: %w", err)
		}

		return view, nil
	})
}

// StartOrchestration starts the orchestration workflow for the derived
// fulfillment.
func (s *CreateManagedResourceWorkflowSpec) StartOrchestration() Activity[FulfillmentID, struct{}] {
	return NewActivity("start-mr-orchestration", func(ctx context.Context, id FulfillmentID) (struct{}, error) {
		tx, err := s.Store.BeginReadOnly(ctx)
		if err != nil {
			return struct{}{}, err
		}
		defer tx.RollbackOnExit()
		f, err := tx.Fulfillments().Get(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return struct{}{}, nil // a completed Create replayed after deletion
		}
		if err != nil {
			return struct{}{}, err
		}
		if f.Paused() || f.DeletionReconciled() {
			return struct{}{}, nil
		}
		if err := tx.Commit(); err != nil {
			return struct{}{}, err
		}
		_, err = s.Orchestration.Start(ctx, id)
		if errors.Is(err, ErrAlreadyRunning) {
			return struct{}{}, nil
		}
		return struct{}{}, err
	})
}

// Run is the workflow body: persist everything, then start orchestration.
func (s *CreateManagedResourceWorkflowSpec) Run(record Record, input CreateManagedResourceInput) (ExtensionResourceView, error) {
	input.RequestID = mutationRequestID(record, input.RequestID)
	view, err := RunActivity(record, s.PersistManagedResource(), input)
	if err != nil {
		return ExtensionResourceView{}, fmt.Errorf("persist managed resource: %w", err)
	}

	if _, err := RunActivity(record, s.StartOrchestration(), view.Fulfillment.ID()); err != nil {
		return ExtensionResourceView{}, fmt.Errorf("start orchestration: %w", err)
	}

	return view, nil
}

func replayManagedResourceCreation(ctx context.Context, tx Tx, in CreateManagedResourceInput) (ExtensionResourceView, error) {
	// Create discarded any provisional parent before reporting a nonce replay.
	// This new statement can see the concurrent winner's committed decision,
	// including one that was not visible when our INSERT began.
	decision, err := tx.ExtensionResources().GetCreation(ctx, in.RequestID)
	if err != nil {
		return ExtensionResourceView{}, err
	}
	if !decision.MatchesScope(in.ResourceType, in.Name, mutationActor(in.Auth)) {
		return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: request_id was already used for a different resource or caller", ErrInvalidArgument))
	}
	if decision.AlreadyExists() {
		return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: this Create attempt was rejected", ErrAlreadyExists))
	}
	view, err := tx.ExtensionResources().GetView(ctx, in.ResourceType.FullName(in.Name))
	if errors.Is(err, ErrNotFound) || (err == nil && view.Resource.UID() != decision.UID()) {
		// The accepted incarnation has been retired. Returning a later
		// incarnation would silently rebind a replay to a different owner.
		return ExtensionResourceView{}, TerminalError(ErrCreationRetired)
	}
	if err != nil {
		return ExtensionResourceView{}, err
	}
	return view, tx.Commit()
}
