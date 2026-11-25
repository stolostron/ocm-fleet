package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"time"
)

// UpdateManagedResourceInput replaces the selected mutable fields. The spec
// is one ownership unit: replacing it also replaces the derived intent.
type UpdateManagedResourceInput struct {
	RequestID          string
	ResourceType       ResourceType
	Name               ResourceName
	Spec               json.RawMessage
	Labels             map[string]string
	UpdateSpec         bool
	UpdateLabels       bool
	Etag               Etag
	ExpectedGeneration Generation
	UserSignature      []byte
	ValidUntil         time.Time
	Auth               DeliveryAuth
}

// UpdateManagedResourceWorkflowSpec atomically records conditional intent and
// hands pending intent to orchestration. The durable workflow retains that
// obligation even when a lost activity result replays against its own stale
// etag; it needs neither mutation receipts nor a maintenance sweep to recover.
type UpdateManagedResourceWorkflowSpec struct {
	Store         Store
	Orchestration OrchestrationWorkflow
	ProvenanceSvc *ProvenanceService
	Now           func() time.Time
	RetryDelay    time.Duration // delay after an exhausted activity retry budget; defaults to 5s
}

func (s *UpdateManagedResourceWorkflowSpec) Name() string { return "update-managed-resource" }

func (s *UpdateManagedResourceWorkflowSpec) Run(record Record, state UpdateManagedResourceState) (ExtensionResourceView, error) {
	if state.Target == nil {
		target, err := RunActivity(record, s.PrepareUpdate(), state.Input)
		if err != nil {
			if IsTerminal(err) || IsAuthExpired(err) {
				// No mutation has been scheduled, so there is no uncertain
				// write to settle or committed work to hand off.
				return ExtensionResourceView{}, err
			}
			return ExtensionResourceView{}, s.retry(record, state)
		}
		state.Target = &target
	}
	if state.Result == nil {
		view, err := RunActivity(record, s.MutateSpec(), PreparedManagedResourceUpdate{Input: state.Input, Target: *state.Target})
		if err != nil && !IsTerminal(err) && !IsAuthExpired(err) {
			// Exhausting the engine's activity retry budget does not
			// discharge an ambiguous mutation. Keep its original fence.
			return ExtensionResourceView{}, s.retry(record, state)
		}
		state.Result = managedResourceUpdateResult(view, err)
	}
	if !state.Settled {
		if state.Result.Failure != "" {
			if _, err := RunActivity(record, s.FenceAttempt(), *state.Target); err != nil {
				return ExtensionResourceView{}, s.retry(record, state)
			}
		}
		// A changed successful mutation retires its revision atomically.
		// A known successful no-op leaves no write to fence.
		state.Settled = true
	}

	// Bound history even if an older orchestration takes a long time to
	// release its instance ID. A fresh run retains the resolved API outcome.
	for range 32 {
		f, err := RunActivity(record, s.LoadProgress(), *state.Target)
		if err != nil {
			return ExtensionResourceView{}, s.retry(record, state)
		}
		if f == nil || f.State() == FulfillmentStateDeleting || f.Paused() || f.ObservedGeneration() >= f.Generation() {
			return state.Result.View, state.Result.err()
		}
		started, err := RunActivity(record, s.StartOrchestration(), state.Target.FulfillmentID)
		if err != nil {
			return ExtensionResourceView{}, s.retry(record, state)
		}
		if started {
			return state.Result.View, state.Result.err()
		}
		// AlreadyRunning can refer to an older workflow just about to
		// exit. Recheck instead of assuming it owns the current work.
		if err := record.Sleep(convergencePollInterval); err != nil {
			return ExtensionResourceView{}, err
		}
	}
	return ExtensionResourceView{}, ContinueAsNew(state)
}

// PrepareUpdate durably captures identity and freshness before an activity can
// write. A retry must not turn a conditional mutation into an unconditional
// overwrite, nor retarget a replacement resource with the same name.
func (s *UpdateManagedResourceWorkflowSpec) PrepareUpdate() Activity[UpdateManagedResourceInput, ManagedResourceUpdateTarget] {
	return NewActivity("mr-prepare-update", func(ctx context.Context, in UpdateManagedResourceInput) (ManagedResourceUpdateTarget, error) {
		tx, err := s.Store.BeginReadOnly(ctx)
		if err != nil {
			return ManagedResourceUpdateTarget{}, err
		}
		defer tx.RollbackOnExit()
		view, err := tx.ExtensionResources().GetView(ctx, in.ResourceType.FullName(in.Name))
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return ManagedResourceUpdateTarget{}, TerminalError(err)
			}
			return ManagedResourceUpdateTarget{}, err
		}
		if view.Resource.ResourceType() != in.ResourceType || view.Resource.Managed() == nil || view.Fulfillment == nil {
			return ManagedResourceUpdateTarget{}, TerminalError(fmt.Errorf("%w: resource has no managed state for this type", ErrInvalidArgument))
		}
		if in.Etag != "" && view.Etag() != in.Etag {
			return ManagedResourceUpdateTarget{}, TerminalError(fmt.Errorf("%w: etag mismatch", ErrStaleGeneration))
		}
		if view.Fulfillment.State() == FulfillmentStateDeleting {
			return ManagedResourceUpdateTarget{}, TerminalError(fmt.Errorf("%w: resource is deleting", ErrInvalidArgument))
		}
		return ManagedResourceUpdateTarget{
			ResourceType: in.ResourceType, Name: in.Name, UID: view.Resource.UID(),
			FulfillmentID: view.Resource.Managed().FulfillmentID(),
			Revision:      view.Resource.Revision(), Etag: view.Etag(),
		}, tx.Commit()
	})
}

// MutateSpec checks freshness while holding the aggregate's write lock and
// persists the intent, strategies, and retired activity revision in one
// transaction. Every attempt uses the target captured before the first write.
func (s *UpdateManagedResourceWorkflowSpec) MutateSpec() Activity[PreparedManagedResourceUpdate, ExtensionResourceView] {
	return NewActivity("mr-update-spec", func(ctx context.Context, prepared PreparedManagedResourceUpdate) (ExtensionResourceView, error) {
		in, target := prepared.Input, prepared.Target
		tx, err := s.Store.Begin(ctx)
		if err != nil {
			return ExtensionResourceView{}, err
		}
		defer tx.RollbackOnExit()
		er, err := tx.ExtensionResources().Get(ctx, in.ResourceType.FullName(in.Name))
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return ExtensionResourceView{}, TerminalError(err)
			}
			return ExtensionResourceView{}, err
		}
		if er.ResourceType() != in.ResourceType || er.Managed() == nil {
			return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: resource has no managed state for this type", ErrInvalidArgument))
		}
		if er.UID() != target.UID || er.Managed().FulfillmentID() != target.FulfillmentID || er.Revision() != target.Revision {
			return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: update target or revision changed", ErrStaleGeneration))
		}
		f, err := tx.Fulfillments().Get(ctx, er.Managed().FulfillmentID())
		if err != nil {
			return ExtensionResourceView{}, err
		}
		intent, err := tx.ExtensionResources().GetIntent(ctx, er.UID(), er.Managed().CurrentVersion())
		if err != nil {
			return ExtensionResourceView{}, err
		}
		view := ExtensionResourceView{Resource: *er, Intent: &intent, Fulfillment: f}
		if view.Etag() != target.Etag {
			return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: etag mismatch", ErrStaleGeneration))
		}
		if f.State() == FulfillmentStateDeleting {
			return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: resource is deleting", ErrInvalidArgument))
		}
		specChanged := false
		if in.UpdateSpec {
			before, err := canonicalJSON(intent.Spec)
			if err != nil {
				return ExtensionResourceView{}, err
			}
			after, err := canonicalJSON(in.Spec)
			if err != nil {
				return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: invalid spec: %v", ErrInvalidArgument, err))
			}
			specChanged = !bytes.Equal(before, after)
		}
		labelsChanged := in.UpdateLabels && !maps.Equal(er.Labels(), in.Labels)
		now := s.now()
		if specChanged {
			nextGen := f.Generation() + 1
			if in.ExpectedGeneration != 0 && in.ExpectedGeneration != nextGen {
				return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: expected_generation mismatch", ErrStaleGeneration))
			}
			var prov *Provenance
			var attestRef *AttestationRef
			if f.Provenance() != nil || len(in.UserSignature) > 0 {
				if len(in.UserSignature) == 0 || in.ExpectedGeneration == 0 {
					return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: updating signed intent requires a new signature and expected_generation", ErrInvalidArgument))
				}
				if s.ProvenanceSvc == nil {
					return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: signed updates require a provenance service", ErrInvalidArgument))
				}
				prov, err = s.ProvenanceSvc.BuildManagedResourceProvenance(ctx, tx.SignerEnrollments(), in.Auth.Caller,
					in.ResourceType, in.Name, in.Spec, in.ExpectedGeneration, in.UserSignature, in.ValidUntil)
				if err != nil {
					if errors.Is(err, ErrInvalidArgument) {
						return ExtensionResourceView{}, TerminalError(err)
					}
					return ExtensionResourceView{}, err
				}
				rt := in.ResourceType
				attestRef = &AttestationRef{RelationRef: &rt}
			}
			typeDef, err := tx.ExtensionResources().GetType(ctx, in.ResourceType)
			if err != nil {
				return ExtensionResourceView{}, err
			}
			if typeDef.Management() == nil {
				return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: resource type is no longer managed", ErrInvalidArgument))
			}
			intent, err = er.RecordIntent(in.Spec, now)
			if err != nil {
				return ExtensionResourceView{}, err
			}
			ms, ps, rs := typeDef.Management().Relation().DeriveStrategies(intent)
			f.AdvanceManifestStrategy(ms, now)
			if !reflect.DeepEqual(ps, f.PlacementStrategy()) {
				f.AdvancePlacementStrategy(ps, now)
			}
			if !reflect.DeepEqual(rs, f.RolloutStrategy()) {
				f.AdvanceRolloutStrategy(rs, now)
			}
			if err := f.UpdateIntent(in.Auth, prov, attestRef, now); err != nil {
				return ExtensionResourceView{}, TerminalError(err)
			}
			if err := tx.Fulfillments().Update(ctx, f); err != nil {
				return ExtensionResourceView{}, err
			}
		}
		if labelsChanged {
			er.SetLabels(in.Labels, now)
		}
		if specChanged || labelsChanged {
			if err := tx.ExtensionResources().Update(ctx, er); err != nil {
				if errors.Is(err, ErrStaleGeneration) {
					return ExtensionResourceView{}, TerminalError(err)
				}
				return ExtensionResourceView{}, err
			}
		}
		view = ExtensionResourceView{Resource: *er, Intent: &intent, Fulfillment: f}
		return view, tx.Commit()
	})
}

// FenceAttempt prevents delayed copies of a rejected or ambiguous attempt from
// committing after this workflow has checked progress and returned. It changes
// only the current revision; desired intent and generation stay untouched.
func (s *UpdateManagedResourceWorkflowSpec) FenceAttempt() Activity[ManagedResourceUpdateTarget, struct{}] {
	return NewActivity("mr-fence-update", func(ctx context.Context, target ManagedResourceUpdateTarget) (struct{}, error) {
		tx, err := s.Store.Begin(ctx)
		if err != nil {
			return struct{}{}, err
		}
		defer tx.RollbackOnExit()
		if err := tx.ExtensionResources().FenceUpdate(ctx, target.UID, target.Revision); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, tx.Commit()
	})
}

// LoadProgress reads the latest work for the pinned incarnation, rather than
// relying on the lost mutation response to identify a particular generation.
func (s *UpdateManagedResourceWorkflowSpec) LoadProgress() Activity[ManagedResourceUpdateTarget, *Fulfillment] {
	return NewActivity("mr-load-update-progress", func(ctx context.Context, target ManagedResourceUpdateTarget) (*Fulfillment, error) {
		tx, err := s.Store.BeginReadOnly(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.RollbackOnExit()
		er, err := tx.ExtensionResources().Get(ctx, target.ResourceType.FullName(target.Name))
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if er.UID() != target.UID || er.Managed() == nil || er.Managed().FulfillmentID() != target.FulfillmentID {
			return nil, nil
		}
		f, err := tx.Fulfillments().Get(ctx, target.FulfillmentID)
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return f, tx.Commit()
	})
}

// StartOrchestration checkpoints the durable handoff as an activity. A lost
// successful Start may replay as AlreadyRunning; the workflow then rechecks
// progress until it can prove work completed or start a fresh owner.
func (s *UpdateManagedResourceWorkflowSpec) StartOrchestration() Activity[FulfillmentID, bool] {
	return NewActivity("mr-start-update-orchestration", func(ctx context.Context, id FulfillmentID) (bool, error) {
		_, err := s.Orchestration.Start(ctx, id)
		if errors.Is(err, ErrAlreadyRunning) {
			return false, nil
		}
		return err == nil, err
	})
}

func (s *UpdateManagedResourceWorkflowSpec) retry(record Record, state UpdateManagedResourceState) error {
	delay := s.RetryDelay
	if delay <= 0 {
		delay = 5 * time.Second
	}
	if err := record.Sleep(delay); err != nil {
		return err
	}
	return ContinueAsNew(state)
}

func (s *UpdateManagedResourceWorkflowSpec) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
