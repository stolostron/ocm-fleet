package domain

import (
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
// hands its generation to orchestration. Current generation gaps also remain
// discoverable by maintenance if replay encounters its own now-stale etag.
type UpdateManagedResourceWorkflowSpec struct {
	Store         Store
	Orchestration OrchestrationWorkflow
	ProvenanceSvc *ProvenanceService
	Now           func() time.Time
}

func (s *UpdateManagedResourceWorkflowSpec) Name() string { return "update-managed-resource" }

func (s *UpdateManagedResourceWorkflowSpec) Run(record Record, input UpdateManagedResourceInput) (ExtensionResourceView, error) {
	view, err := RunActivity(record, s.MutateSpec(), input)
	if err != nil {
		return ExtensionResourceView{}, err
	}
	// Deletion may supersede this update between the two activities.
	if err := convergenceLoop(record, s.Orchestration, s.LoadFulfillment(), view.Fulfillment.ID(), view.Fulfillment.Generation(), true); err != nil {
		return ExtensionResourceView{}, err
	}
	return view, nil
}

// MutateSpec checks freshness while holding the aggregate's write lock and
// persists the intent and strategies in one transaction.
func (s *UpdateManagedResourceWorkflowSpec) MutateSpec() Activity[UpdateManagedResourceInput, ExtensionResourceView] {
	return NewActivity("mr-update-spec", func(ctx context.Context, in UpdateManagedResourceInput) (ExtensionResourceView, error) {
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
		f, err := tx.Fulfillments().Get(ctx, er.Managed().FulfillmentID())
		if err != nil {
			return ExtensionResourceView{}, err
		}
		intent, err := tx.ExtensionResources().GetIntent(ctx, er.UID(), er.Managed().CurrentVersion())
		if err != nil {
			return ExtensionResourceView{}, err
		}
		view := ExtensionResourceView{Resource: *er, Intent: &intent, Fulfillment: f}
		if in.Etag != "" && view.Etag() != in.Etag {
			return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: etag mismatch", ErrStaleGeneration))
		}
		if f.State() == FulfillmentStateDeleting {
			return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: resource is deleting", ErrInvalidArgument))
		}
		specChanged := false
		if in.UpdateSpec {
			before, err := mutationFingerprint(intent.Spec)
			if err != nil {
				return ExtensionResourceView{}, err
			}
			after, err := mutationFingerprint(in.Spec)
			if err != nil {
				return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: invalid spec: %v", ErrInvalidArgument, err))
			}
			specChanged = before != after
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
				return ExtensionResourceView{}, err
			}
		}
		view = ExtensionResourceView{Resource: *er, Intent: &intent, Fulfillment: f}
		return view, tx.Commit()
	})
}

func (s *UpdateManagedResourceWorkflowSpec) LoadFulfillment() Activity[FulfillmentID, *Fulfillment] {
	return NewActivity("mr-load-fulfillment-for-update", func(ctx context.Context, id FulfillmentID) (*Fulfillment, error) {
		tx, err := s.Store.BeginReadOnly(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.RollbackOnExit()
		f, err := tx.Fulfillments().Get(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return f, tx.Commit()
	})
}

func (s *UpdateManagedResourceWorkflowSpec) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
