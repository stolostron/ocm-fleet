package goworkflows_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/cschleiden/go-workflows/backend"
	"github.com/cschleiden/go-workflows/backend/history"
	wfsqlite "github.com/cschleiden/go-workflows/backend/sqlite"
	"github.com/cschleiden/go-workflows/client"
	"github.com/cschleiden/go-workflows/workflow"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
	"github.com/stolostron/ocm-fleet/server/internal/domain/workflowenginetest"
	"github.com/stolostron/ocm-fleet/server/internal/infrastructure/goworkflows"
)

func TestUpdateManagedResource_ContinuationsDoNotGrowParentHistory(t *testing.T) {
	var baseline []history.EventType
	for _, failures := range []int{0, 8} {
		t.Run(fmt.Sprintf("%d continuations", failures), func(t *testing.T) {
			infra := goInfra(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
			typeDef := domain.NewExtensionResourceType("records.example.io/Record", "v1alpha", "records", now,
				domain.WithManagement(domain.NewRegisteredSelfTarget("record-local", "managed.record"), domain.Signature{}))
			tx, err := infra.Store.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.RollbackOnExit()
			if err := tx.ExtensionResources().CreateType(ctx, typeDef); err != nil {
				t.Fatal(err)
			}
			if err := tx.Targets().Create(ctx, domain.TargetInfoFromSnapshot(domain.TargetInfoSnapshot{ID: "record-local", Type: workflowenginetest.TestTargetType})); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			create := &domain.CreateManagedResourceWorkflowSpec{Store: infra.Store}
			initial, err := create.PersistManagedResource().Run(ctx, domain.CreateManagedResourceInput{
				RequestID: "seed-history", ResourceType: typeDef.ResourceType(), Name: "records/history", TypeDef: typeDef, Spec: json.RawMessage(`{}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			persisted := wfsqlite.NewInMemoryBackend()
			faults := &unavailableOrchestrationBackend{Backend: persisted, fulfillmentID: initial.Fulfillment.ID(), remaining: failures}
			reg := &goworkflows.Registry{
				Worker: startWorker(t, faults), Client: client.New(faults), Timeout: 10 * time.Second,
				ActivityOptions: &workflow.ActivityOptions{RetryOptions: workflow.RetryOptions{MaxAttempts: 1}},
			}
			infra.DeliveryReporterFactory(reg)
			orch, err := reg.RegisterOrchestration(domain.NewOrchestrationWorkflowSpec(infra.Store, infra.Delivery, domain.StrategyFactory{Store: infra.Store}, reg, domain.WithVault(infra.Vault)))
			if err != nil {
				t.Fatal(err)
			}
			update, err := reg.RegisterUpdateManagedResource(&domain.UpdateManagedResourceWorkflowSpec{Store: infra.Store, Orchestration: orch, RetryDelay: time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			execution, err := update.Start(ctx, domain.UpdateManagedResourceInput{
				RequestID: "history", ResourceType: typeDef.ResourceType(), Name: initial.Resource.Name(),
				Spec: json.RawMessage(`{"message":"after"}`), UpdateSpec: true, Etag: initial.Etag(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := execution.AwaitResult(ctx); err != nil {
				t.Fatal(err)
			}
			instances, err := persisted.GetWorkflowInstances(ctx, "", "", 100)
			if err != nil {
				t.Fatal(err)
			}
			var parentEvents []history.EventType
			childRuns := 0
			for _, ref := range instances {
				if ref.Instance.Parent != nil && ref.Instance.Parent.InstanceID == execution.WorkflowID() {
					childRuns++
				}
				if ref.Instance.InstanceID != execution.WorkflowID() {
					continue
				}
				events, err := persisted.GetWorkflowInstanceHistory(ctx, ref.Instance, nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range events {
					parentEvents = append(parentEvents, event.Type)
				}
			}
			if childRuns != failures+1 {
				t.Fatalf("expected %d continued child executions, got %d", failures+1, childRuns)
			}
			if !slices.Contains(parentEvents, history.EventType_SubWorkflowScheduled) || !slices.Contains(parentEvents, history.EventType_SubWorkflowCompleted) {
				t.Fatalf("parent did not await its child's final result: %v", parentEvents)
			}
			if failures == 0 {
				baseline = parentEvents
			} else if !slices.Equal(parentEvents, baseline) {
				t.Fatalf("continuations grew parent history: baseline %v, actual %v", baseline, parentEvents)
			}
			t.Logf("%d child executions, %d parent history events", childRuns, len(parentEvents))
		})
	}
}

// unavailableOrchestrationBackend makes each failed handoff exhaust a one-attempt
// activity budget, forcing a durable continuation through the real adapter.
type unavailableOrchestrationBackend struct {
	backend.Backend
	mu            sync.Mutex
	fulfillmentID domain.FulfillmentID
	remaining     int
}

func (b *unavailableOrchestrationBackend) CreateWorkflowInstance(ctx context.Context, instance *workflow.Instance, event *history.Event) error {
	b.mu.Lock()
	if instance.InstanceID == string(b.fulfillmentID) && b.remaining > 0 {
		b.remaining--
		b.mu.Unlock()
		return errors.New("injected orchestration backend outage")
	}
	b.mu.Unlock()
	return b.Backend.CreateWorkflowInstance(ctx, instance, event)
}
