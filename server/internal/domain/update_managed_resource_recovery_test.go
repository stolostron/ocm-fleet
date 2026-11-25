package domain_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/application"
	"github.com/stolostron/ocm-fleet/server/internal/domain"
	"github.com/stolostron/ocm-fleet/server/internal/infrastructure/delivery"
	"github.com/stolostron/ocm-fleet/server/internal/infrastructure/memworkflow"
	"github.com/stolostron/ocm-fleet/server/internal/infrastructure/sqlite"
)

func TestUpdateManagedResource_RecoversLostMutationResultWithoutMaintenance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newUpdateRecoveryHarness(t)
		record := &replayedMutationRecord{stubRecord: stubRecord{ctx: context.Background()}}
		_, err := h.update.Run(record, domain.UpdateManagedResourceState{Input: h.input})
		if !errors.Is(err, domain.ErrStaleGeneration) {
			t.Fatalf("replaying a committed conditional write must retain its conflict: %v", err)
		}
		if !record.replayed {
			t.Fatal("test did not lose the mutation's first result")
		}
		synctest.Wait()
		current := h.view(t)
		if current.Intent.Version != 2 || current.Fulfillment.Generation() != 2 {
			t.Fatalf("lost result changed the committed intent: %+v", current)
		}
		if current.Fulfillment.ObservedGeneration() != current.Fulfillment.Generation() {
			t.Fatalf("update left committed work without an owner: observed %d, desired %d", current.Fulfillment.ObservedGeneration(), current.Fulfillment.Generation())
		}
	})
}

func TestUpdateManagedResource_FencesDelayedAttemptBeforeReturningFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newUpdateRecoveryHarness(t)
		before := h.view(t)
		record := &rejectedMutationRecord{stubRecord: stubRecord{ctx: context.Background()}}
		_, err := h.update.Run(record, domain.UpdateManagedResourceState{Input: h.input})
		if !errors.Is(err, domain.ErrStaleGeneration) {
			t.Fatalf("conditional rejection was lost: %v", err)
		}
		// Another copy of this activity may still be running after the
		// engine has recorded the rejection. It must no longer be able to
		// commit, even though no mutation advanced the desired generation.
		if _, err := h.update.MutateSpec().Run(context.Background(), record.prepared); !errors.Is(err, domain.ErrStaleGeneration) {
			t.Fatalf("delayed activity escaped the retired revision: %v", err)
		}
		synctest.Wait()
		after := h.view(t)
		if after.Intent.Version != before.Intent.Version || after.Fulfillment.Generation() != before.Fulfillment.Generation() || !after.Resource.UpdatedAt().Equal(before.Resource.UpdatedAt()) {
			t.Fatal("settling a failed attempt changed desired intent or its timestamp")
		}
		if after.Resource.Revision() != before.Resource.Revision()+1 || after.Fulfillment.ObservedGeneration() != after.Fulfillment.Generation() {
			t.Fatal("workflow did not retire the attempt and hand off existing pending work")
		}
		if _, err := h.update.FenceAttempt().Run(context.Background(), record.prepared.Target); err != nil {
			t.Fatal(err)
		}
		if h.view(t).Etag() != after.Etag() {
			t.Fatal("replaying a fence changed an already retired revision")
		}
	})
}

func TestUpdateManagedResource_RecoveryPreservesNewerIntent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newUpdateRecoveryHarness(t)
		target := h.prepare(t, h.input)
		newer := h.input
		newer.Spec = json.RawMessage(`{"message":"newer writer"}`)
		if _, err := h.update.MutateSpec().Run(context.Background(), domain.PreparedManagedResourceUpdate{Input: newer, Target: target}); err != nil {
			t.Fatal(err)
		}
		_, err := h.update.Run(&stubRecord{ctx: context.Background()}, domain.UpdateManagedResourceState{Input: h.input, Target: &target})
		if !errors.Is(err, domain.ErrStaleGeneration) {
			t.Fatalf("older mutation did not retain its conflict: %v", err)
		}
		synctest.Wait()
		current := h.view(t)
		if string(current.Intent.Spec) != string(newer.Spec) || current.Intent.Version != 2 || current.Resource.Revision() != target.Revision+1 {
			t.Fatal("recovery overwrote or refenced a newer update")
		}
		if current.Fulfillment.ObservedGeneration() != current.Fulfillment.Generation() {
			t.Fatal("recovery did not dispatch the latest generation")
		}
	})
}

func TestUpdateManagedResource_RecoveryDoesNotTouchReplacement(t *testing.T) {
	h := newUpdateRecoveryHarness(t)
	target := h.prepare(t, h.input)
	ctx := context.Background()
	cleanup := &domain.DeleteManagedResourceCleanupWorkflowSpec{Store: h.store}
	if _, err := cleanup.DeleteManagedResourceAndFulfillment().Run(ctx, domain.DeleteManagedResourceCleanupInput{
		ResourceType: target.ResourceType, Name: target.Name, FulfillmentID: target.FulfillmentID,
	}); err != nil {
		t.Fatal(err)
	}
	tx, err := h.store.BeginReadOnly(ctx)
	if err != nil {
		t.Fatal(err)
	}
	typeDef, err := tx.ExtensionResources().GetType(ctx, h.input.ResourceType)
	tx.RollbackOnExit()
	if err != nil {
		t.Fatal(err)
	}
	create := &domain.CreateManagedResourceWorkflowSpec{Store: h.store}
	replacement, err := create.PersistManagedResource().Run(ctx, domain.CreateManagedResourceInput{
		RequestID: "replacement", ResourceType: target.ResourceType, Name: target.Name, TypeDef: typeDef, Spec: json.RawMessage(`{"replacement":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.update.Run(&stubRecord{ctx: ctx}, domain.UpdateManagedResourceState{Input: h.input, Target: &target}); !errors.Is(err, domain.ErrStaleGeneration) {
		t.Fatalf("old update retargeted a reused name: %v", err)
	}
	if current := h.view(t); current.Etag() != replacement.Etag() || current.Fulfillment.ObservedGeneration() != 0 {
		t.Fatal("old workflow mutated or dispatched the replacement")
	}
}

func TestUpdateManagedResource_LabelRoundTripDoesNotReviveOldPrecondition(t *testing.T) {
	h := newUpdateRecoveryHarness(t)
	target := h.prepare(t, h.input)
	initial := h.view(t)
	for _, labels := range []map[string]string{{"phase": "temporary"}, nil} {
		input := h.input
		input.UpdateSpec, input.UpdateLabels, input.Labels = false, true, labels
		input.Etag = h.view(t).Etag()
		prepared := domain.PreparedManagedResourceUpdate{Input: input, Target: h.prepare(t, input)}
		if _, err := h.update.MutateSpec().Run(context.Background(), prepared); err != nil {
			t.Fatal(err)
		}
	}
	current := h.view(t)
	if current.Intent.Version != initial.Intent.Version || current.Fulfillment.Generation() != initial.Fulfillment.Generation() {
		t.Fatal("labels-only updates changed intent or fulfillment generation")
	}
	if current.Etag() == initial.Etag() || current.Resource.Revision() != initial.Resource.Revision()+2 {
		t.Fatal("restoring the visible values revived an old precondition")
	}
	if _, err := h.update.MutateSpec().Run(context.Background(), domain.PreparedManagedResourceUpdate{Input: h.input, Target: target}); !errors.Is(err, domain.ErrStaleGeneration) {
		t.Fatalf("delayed update applied after a label round trip: %v", err)
	}
}

func TestUpdateManagedResource_FailedSaveRollsBackIntentStrategiesAndFence(t *testing.T) {
	h := newUpdateRecoveryHarness(t)
	before := h.view(t)
	prepared := domain.PreparedManagedResourceUpdate{Input: h.input, Target: h.prepare(t, h.input)}
	db := h.store.(*sqlite.Store).DB
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER fail_update BEFORE UPDATE ON extension_resource_managed
BEGIN SELECT RAISE(ABORT, 'injected aggregate save failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.update.MutateSpec().Run(ctx, prepared); err == nil || domain.IsTerminal(err) {
		t.Fatalf("storage failure must remain retryable: %v", err)
	}
	if current := h.view(t); current.Etag() != before.Etag() || current.Resource.Revision() != before.Resource.Revision() {
		t.Fatal("failed save committed intent, derived strategies, or a retired revision")
	}
	if _, err := db.ExecContext(ctx, "DROP TRIGGER fail_update"); err != nil {
		t.Fatal(err)
	}
	result, err := h.update.MutateSpec().Run(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if result.Intent.Version != 2 || result.Resource.Revision() != before.Resource.Revision()+1 || result.Fulfillment.Generation() != 2 {
		t.Fatal("original attempt could not commit cleanly after rollback")
	}
}

func TestUpdateManagedResource_HandoffRetryPreservesResolvedOutcome(t *testing.T) {
	for _, loseResult := range []bool{false, true} {
		name := "success"
		if loseResult {
			name = "conflict after lost result"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				h := newUpdateRecoveryHarness(t)
				var mutationRecord domain.Record = &stubRecord{ctx: context.Background()}
				if loseResult {
					mutationRecord = &replayedMutationRecord{stubRecord: stubRecord{ctx: context.Background()}}
				}
				record := &failedHandoffRecord{Record: mutationRecord}
				_, err := h.update.Run(record, domain.UpdateManagedResourceState{Input: h.input})
				var can *domain.ContinueAsNewError
				if !errors.As(err, &can) {
					t.Fatalf("exhausted activity retries must retain the workflow: %v", err)
				}
				// Exercise the same serialization boundary as a durable restart.
				data, err := json.Marshal(can.Input)
				if err != nil {
					t.Fatal(err)
				}
				var state domain.UpdateManagedResourceState
				if err := json.Unmarshal(data, &state); err != nil {
					t.Fatal(err)
				}
				if state.Target == nil || state.Target.Etag != h.input.Etag || state.Result == nil || !state.Settled {
					t.Fatal("restart lost the original target or resolved mutation")
				}
				resumed := &handoffOnlyRecord{stubRecord: stubRecord{ctx: context.Background()}, t: t}
				view, err := h.update.Run(resumed, state)
				if loseResult {
					if !errors.Is(err, domain.ErrStaleGeneration) {
						t.Fatalf("restart lost the conflict classification: %v", err)
					}
				} else if err != nil || view.Intent == nil || view.Intent.Version != 2 {
					t.Fatalf("restart lost the successful result: %+v, %v", view, err)
				}
				synctest.Wait()
				current := h.view(t)
				if current.Intent.Version != 2 || current.Resource.Revision() != state.Target.Revision+1 || current.Fulfillment.ObservedGeneration() != 2 {
					t.Fatal("handoff retry reapplied intent or left work undispatched")
				}
			})
		})
	}
}

func TestUpdateManagedResource_WaitsForExitingOlderOrchestration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newUpdateRecoveryHarness(t)
		barrier := &reconciliationCompletionBarrier{
			Store: h.store, committed: make(chan struct{}), release: make(chan struct{}),
		}
		h.orchestration.Store = barrier
		initial := h.view(t)
		older, err := h.update.Orchestration.Start(context.Background(), initial.Fulfillment.ID())
		if err != nil {
			t.Fatal(err)
		}
		// The old orchestration has committed completion of generation 1
		// and decided to exit. Hold its activity result so its workflow
		// instance remains active while generation 2 is committed.
		<-barrier.committed
		h.input.Etag = h.view(t).Etag()
		record := &busyHandoffRecord{
			stubRecord: stubRecord{ctx: context.Background()}, busy: make(chan struct{}), resume: make(chan struct{}),
		}
		done := make(chan error, 1)
		go func() {
			_, err := h.update.Run(record, domain.UpdateManagedResourceState{Input: h.input})
			done <- err
		}()
		<-record.busy
		select {
		case err := <-done:
			t.Fatalf("AlreadyRunning prematurely discharged the handoff: %v", err)
		default:
		}
		close(barrier.release)
		if _, err := older.AwaitResult(context.Background()); err != nil {
			t.Fatal(err)
		}
		close(record.resume)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if current := h.view(t); current.Fulfillment.Generation() != 2 || current.Fulfillment.ObservedGeneration() != 2 {
			t.Fatal("generation committed after the old exit decision was never dispatched")
		}
	})
}

// replayedMutationRecord discards a successful mutation result and executes the
// same activity input again, as a worker would after losing its completion
// checkpoint. All activities still execute against the real store.
type replayedMutationRecord struct {
	stubRecord
	replayed bool
}

func (r *replayedMutationRecord) Run(activity domain.Activity[any, any], in any) (any, error) {
	out, err := r.stubRecord.Run(activity, in)
	if activity.Name() == "mr-update-spec" && err == nil && !r.replayed {
		r.replayed = true
		return r.stubRecord.Run(activity, in)
	}
	return out, err
}

// rejectedMutationRecord models a definitive rejection from one activity copy
// while retaining another copy's input for a later, delayed execution.
type rejectedMutationRecord struct {
	stubRecord
	prepared domain.PreparedManagedResourceUpdate
}

func (r *rejectedMutationRecord) Run(activity domain.Activity[any, any], in any) (any, error) {
	if activity.Name() == "mr-update-spec" {
		r.prepared = in.(domain.PreparedManagedResourceUpdate)
		return nil, domain.TerminalError(domain.ErrStaleGeneration)
	}
	return r.stubRecord.Run(activity, in)
}

// failedHandoffRecord models an unavailable workflow backend after its finite
// activity retry budget has been exhausted. All preceding database work is real.
type failedHandoffRecord struct{ domain.Record }

func (r *failedHandoffRecord) Run(activity domain.Activity[any, any], in any) (any, error) {
	if activity.Name() == "mr-start-update-orchestration" {
		return nil, errors.New("workflow backend unavailable")
	}
	return r.Record.Run(activity, in)
}

type handoffOnlyRecord struct {
	stubRecord
	t *testing.T
}

func (r *handoffOnlyRecord) Run(activity domain.Activity[any, any], in any) (any, error) {
	if activity.Name() == "mr-update-spec" || activity.Name() == "mr-prepare-update" || activity.Name() == "mr-fence-update" {
		r.t.Fatalf("resolved update repeated %s after ContinueAsNew", activity.Name())
	}
	return r.stubRecord.Run(activity, in)
}

type updateRecoveryHarness struct {
	store         domain.Store
	update        *domain.UpdateManagedResourceWorkflowSpec
	orchestration *domain.OrchestrationWorkflowSpec
	input         domain.UpdateManagedResourceInput
}

func newUpdateRecoveryHarness(t *testing.T) updateRecoveryHarness {
	t.Helper()
	store, vault := setupStore(t)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	ctx := context.Background()
	registry := &memworkflow.Registry{}
	router := delivery.NewRoutingDeliveryService()
	router.Register("record", &sqlite.RecordingDeliveryService{
		Store: store, Reporter: application.NewDeliveryReportService(store, registry), Now: func() time.Time { return now },
	})
	if err := (&application.TargetService{Store: store}).Register(ctx, domain.TargetInfoFromSnapshot(domain.TargetInfoSnapshot{
		ID: "record-local", Type: "record", Name: "Record provider", AcceptedManifestTypes: []domain.ManifestType{"managed.record"},
	})); err != nil {
		t.Fatal(err)
	}
	orchSpec := domain.NewOrchestrationWorkflowSpec(
		store, router, domain.StrategyFactory{Store: store}, registry,
		domain.WithVault(vault), domain.WithAckRetryInterval(5*time.Second),
	)
	orch, err := registry.RegisterOrchestration(orchSpec)
	if err != nil {
		t.Fatal(err)
	}
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Now: func() time.Time { return now }}
	initial, err := create.PersistManagedResource().Run(ctx, domain.CreateManagedResourceInput{
		RequestID: "create-update-recovery", ResourceType: typeDef.ResourceType(), Name: "records/recovery", TypeDef: typeDef, Spec: json.RawMessage(`{"message":"before"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return updateRecoveryHarness{
		store:         store,
		update:        &domain.UpdateManagedResourceWorkflowSpec{Store: store, Orchestration: orch, Now: func() time.Time { return now }},
		orchestration: orchSpec,
		input: domain.UpdateManagedResourceInput{
			RequestID: "update-recovery", ResourceType: typeDef.ResourceType(), Name: initial.Resource.Name(),
			Spec: json.RawMessage(`{"message":"after"}`), UpdateSpec: true, Etag: initial.Etag(),
		},
	}
}

func (h updateRecoveryHarness) view(t *testing.T) domain.ExtensionResourceView {
	t.Helper()
	tx, err := h.store.BeginReadOnly(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	view, err := tx.ExtensionResources().GetView(context.Background(), h.input.ResourceType.FullName(h.input.Name))
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func (h updateRecoveryHarness) prepare(t *testing.T, input domain.UpdateManagedResourceInput) domain.ManagedResourceUpdateTarget {
	t.Helper()
	target, err := h.update.PrepareUpdate().Run(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

// reconciliationCompletionBarrier holds the first completed reconciliation's
// activity result after the real transaction commits. No database lock is held
// while the test commits a concurrent Update and observes AlreadyRunning.
type reconciliationCompletionBarrier struct {
	domain.Store
	once      sync.Once
	committed chan struct{}
	release   chan struct{}
}

func (s *reconciliationCompletionBarrier) Begin(ctx context.Context) (domain.Tx, error) {
	tx, err := s.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &reconciliationCompletionTx{Tx: tx, barrier: s}, nil
}

type reconciliationCompletionTx struct {
	domain.Tx
	barrier  *reconciliationCompletionBarrier
	complete bool
}

func (tx *reconciliationCompletionTx) Fulfillments() domain.FulfillmentRepository {
	return &reconciliationCompletionRepo{FulfillmentRepository: tx.Tx.Fulfillments(), tx: tx}
}

func (tx *reconciliationCompletionTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.complete {
		tx.barrier.once.Do(func() {
			close(tx.barrier.committed)
			<-tx.barrier.release
		})
	}
	return nil
}

type reconciliationCompletionRepo struct {
	domain.FulfillmentRepository
	tx *reconciliationCompletionTx
}

func (r *reconciliationCompletionRepo) Update(ctx context.Context, f *domain.Fulfillment) error {
	r.tx.complete = f.Generation() > 0 && f.ObservedGeneration() >= f.Generation()
	return r.FulfillmentRepository.Update(ctx, f)
}

type busyHandoffRecord struct {
	stubRecord
	busy   chan struct{}
	resume chan struct{}
}

func (r *busyHandoffRecord) Run(activity domain.Activity[any, any], in any) (any, error) {
	out, err := r.stubRecord.Run(activity, in)
	if activity.Name() == "mr-start-update-orchestration" && err == nil && !out.(bool) {
		close(r.busy)
	}
	return out, err
}

func (r *busyHandoffRecord) Sleep(time.Duration) error {
	<-r.resume
	return nil
}
