package workflowenginetest

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

// runUpdateManagedResourceTests exercises the complete registered workflow,
// including activity serialization, finite activity retry budgets, and
// ContinueAsNew. No maintenance service is constructed.
func runUpdateManagedResourceTests(t *testing.T, infraFactory InfraFactory, registryFactory RegistryFactory) {
	for _, test := range []struct {
		name            string
		failedMutations int
		loseCommit      bool
		failedHandoffs  int
	}{
		{name: "ordinary update"},
		{name: "lost commit result", loseCommit: true},
		{name: "mutation retry budget", failedMutations: 4},
		{name: "handoff retry budget", failedHandoffs: 4},
		{name: "conflict survives handoff retry", loseCommit: true, failedHandoffs: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			infra := infraFactory(t)
			reg := registryFactory(t)
			if infra.DeliveryReporterFactory != nil {
				infra.DeliveryReporterFactory(reg)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			registerTargets(ctx, t, infra, "record-local")
			now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
			typeDef := domain.NewExtensionResourceType("records.example.io/Record", "v1alpha", "records", now,
				domain.WithManagement(domain.NewRegisteredSelfTarget("record-local", "managed.record"), domain.Signature{}))
			tx, err := infra.Store.Begin(ctx)
			must(t, err)
			defer tx.RollbackOnExit()
			must(t, tx.ExtensionResources().CreateType(ctx, typeDef))
			must(t, tx.Commit())
			// Seed only the creation transaction: the Update workflow must
			// take responsibility for all pending intent itself.
			create := &domain.CreateManagedResourceWorkflowSpec{Store: infra.Store}
			initial, err := create.PersistManagedResource().Run(ctx, domain.CreateManagedResourceInput{
				RequestID: "seed-update", ResourceType: typeDef.ResourceType(), Name: "records/update", TypeDef: typeDef, Spec: json.RawMessage(`{"message":"before"}`),
			})
			must(t, err)
			completed := make(chan struct{}, 8)
			orch, err := reg.RegisterOrchestration(domain.NewOrchestrationWorkflowSpec(
				infra.Store, infra.Delivery, domain.StrategyFactory{Store: infra.Store}, reg,
				domain.WithVault(infra.Vault), domain.WithFulfillmentObserver(&updateCompletionObserver{completed: completed}),
			))
			must(t, err)
			faults := &managedUpdateFaultStore{Store: infra.Store, failedBegins: test.failedMutations, loseCommit: test.loseCommit}
			handoff := &managedUpdateFaultHandoff{OrchestrationWorkflow: orch, failures: test.failedHandoffs}
			update, err := reg.RegisterUpdateManagedResource(&domain.UpdateManagedResourceWorkflowSpec{
				Store: faults, Orchestration: handoff, RetryDelay: time.Millisecond,
			})
			must(t, err)
			input := domain.UpdateManagedResourceInput{
				RequestID: "update", ResourceType: typeDef.ResourceType(), Name: initial.Resource.Name(),
				Spec: json.RawMessage(`{"message":"after"}`), UpdateSpec: true, Etag: initial.Etag(),
			}
			execution, err := update.Start(ctx, input)
			must(t, err)
			result, err := execution.AwaitResult(ctx)
			if test.loseCommit {
				if !errors.Is(err, domain.ErrStaleGeneration) {
					t.Fatalf("lost conditional result must retain its conflict classification: %v", err)
				}
			} else {
				must(t, err)
				if result.Intent == nil || result.Intent.Version != 2 {
					t.Fatalf("workflow lost its successful result: %+v", result)
				}
			}
			select {
			case <-completed:
			case <-ctx.Done():
				t.Fatal("update failed to dispatch pending intent without maintenance")
			}
			read, err := infra.Store.BeginReadOnly(ctx)
			must(t, err)
			defer read.RollbackOnExit()
			current, err := read.ExtensionResources().GetView(ctx, input.ResourceType.FullName(input.Name))
			must(t, err)
			if current.Intent.Version != 2 || string(current.Intent.Spec) != string(input.Spec) || current.Resource.Revision() != initial.Resource.Revision()+1 {
				t.Fatal("workflow retry applied intent more than once or lost its revision fence")
			}
			if current.Fulfillment.Generation() != 2 || current.Fulfillment.ObservedGeneration() != 2 {
				t.Fatalf("pending generation was not reconciled: %+v", current.Fulfillment.Snapshot())
			}
		})
	}
}

// managedUpdateFaultStore decorates the real store with bounded outages and a
// lost commit acknowledgement. BeginReadOnly is unaffected, so preparation can
// checkpoint before mutation retries exhaust their activity budget.
type managedUpdateFaultStore struct {
	domain.Store
	mu           sync.Mutex
	failedBegins int
	loseCommit   bool
}

func (s *managedUpdateFaultStore) Begin(ctx context.Context) (domain.Tx, error) {
	s.mu.Lock()
	if s.failedBegins > 0 {
		s.failedBegins--
		s.mu.Unlock()
		return nil, errors.New("injected mutation store outage")
	}
	s.mu.Unlock()
	tx, err := s.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &managedUpdateFaultTx{Tx: tx, faults: s}, nil
}

type managedUpdateFaultTx struct {
	domain.Tx
	faults *managedUpdateFaultStore
}

func (tx *managedUpdateFaultTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	tx.faults.mu.Lock()
	defer tx.faults.mu.Unlock()
	if tx.faults.loseCommit {
		tx.faults.loseCommit = false
		return errors.New("injected lost commit acknowledgement")
	}
	return nil
}

type managedUpdateFaultHandoff struct {
	domain.OrchestrationWorkflow
	mu       sync.Mutex
	failures int
}

func (w *managedUpdateFaultHandoff) Start(ctx context.Context, id domain.FulfillmentID) (domain.Execution[struct{}], error) {
	w.mu.Lock()
	if w.failures > 0 {
		w.failures--
		w.mu.Unlock()
		return nil, errors.New("injected workflow backend outage")
	}
	w.mu.Unlock()
	return w.OrchestrationWorkflow.Start(ctx, id)
}

// Activity probes notify after the real reconciliation transaction commits;
// tests can wait for observation without polling or timing assumptions.
type updateCompletionObserver struct {
	domain.NoOpFulfillmentObserver
	completed chan struct{}
}

func (o *updateCompletionObserver) PersistReconciliationStarted(ctx context.Context, _ domain.FulfillmentID) (context.Context, domain.PersistReconciliationProbe) {
	return ctx, &updateCompletionProbe{completed: o.completed}
}

type updateCompletionProbe struct {
	domain.NoOpPersistReconciliationProbe
	completed chan struct{}
}

func (p *updateCompletionProbe) Persisted(_ domain.FulfillmentState, needsRestart bool) {
	if !needsRestart {
		select {
		case p.completed <- struct{}{}:
		default:
		}
	}
}
