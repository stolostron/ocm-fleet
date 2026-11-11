package scripted_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stolostron/ocm-fleet/server/internal/addon/scripted"
	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func TestAgent_ResultAllowsNextAttemptBeforeReportReturns(t *testing.T) {
	for _, operation := range []string{"delivery", "removal"} {
		for _, outcome := range []string{"ack failure", "completion failure", "success"} {
			t.Run(operation+"/"+outcome, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					s := newRetryScenario(t, operation, outcome)
					releaseReport := s.holdResult("first")
					defer releaseReport()

					s.dispatch(t, "first")
					s.result(t, s.firstResult)
					// The platform can retry as soon as the result is visible,
					// even while ReportResult remains blocked.
					s.dispatch(t, "retry")
					s.result(t, domain.DeliveryStateDelivered)
					s.observer.attempts["retry"].waitEnded(t)
					releaseReport()
					s.observer.attempts["first"].waitEnded(t)
				})
			})
		}
	}
}

func TestAgent_PreviousAttemptCleanupPreservesRetry(t *testing.T) {
	for _, operation := range []string{"delivery", "removal"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newRetryScenario(t, operation, "success")
				releaseReport := s.holdResult("first")
				defer releaseReport()
				releaseAck := s.observer.attempts["retry"].holdAcknowledgement()
				defer releaseAck()

				s.dispatch(t, "first")
				s.result(t, domain.DeliveryStateDelivered)
				s.dispatch(t, "retry")
				s.observer.attempts["retry"].waitAcknowledged(t)
				releaseReport()
				s.observer.attempts["first"].waitEnded(t)

				// The first attempt has finished all cleanup; the retry still
				// owns this resource's slot while its acknowledgement probe waits.
				if err := s.send("conflict"); err != domain.ErrInvalidArgument {
					t.Fatalf("dispatch while retry is active: %v, want ErrInvalidArgument", err)
				}
				releaseAck()
				s.result(t, domain.DeliveryStateDelivered)
				s.observer.attempts["retry"].waitEnded(t)
			})
		})
	}
}

func TestAgent_CloseWaitsForResultReporting(t *testing.T) {
	for _, operation := range []string{"delivery", "removal"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newRetryScenario(t, operation, "success")
				releaseReport := s.holdResult("first")
				defer releaseReport()
				s.dispatch(t, "first")
				s.result(t, domain.DeliveryStateDelivered)

				closed := make(chan error, 1)
				go func() { closed <- s.agent.Close(t.Context()) }()
				synctest.Wait()
				select {
				case err := <-closed:
					t.Fatalf("Close returned before result reporting finished: %v", err)
				default:
				}

				releaseReport()
				s.observer.attempts["first"].waitEnded(t)
				if err := <-closed; err != nil {
					t.Fatalf("Close: %v", err)
				}
			})
		})
	}
}

type retryScenario struct {
	agent       *scripted.Agent
	reporter    *channelReporter
	observer    *attemptObserver
	send        func(domain.DeliveryID) error
	firstResult domain.DeliveryState
}

func newRetryScenario(t *testing.T, operation, outcome string) *retryScenario {
	t.Helper()
	reporter := newChannelReporter()
	reporter.resultGates = make(map[domain.DeliveryID]<-chan struct{})
	observer := &attemptObserver{attempts: map[domain.DeliveryID]*attemptProbe{
		"first": newAttemptProbe(),
		"retry": newAttemptProbe(),
	}}
	agent := newTestAgent(t, reporter, nopInventory{}, scripted.WithObserver(observer))
	dispatch := agent.Deliver
	if operation == "removal" {
		dispatch = agent.Remove
	}

	ack, completion := "SUCCESS", "SUCCESS"
	want := domain.DeliveryStateDelivered
	switch outcome {
	case "ack failure":
		ack, want = "FAILURE", domain.DeliveryStateFailed
	case "completion failure":
		completion, want = "FAILURE", domain.DeliveryStateFailed
	}
	_, raw := wrapSpec(t, json.RawMessage(fmt.Sprintf(`{
		"behavior": {%q: {
			"acknowledgement": {"outcome": {"sequence": {"values": [%q, "SUCCESS"]}}},
			"completion": {"outcome": {"sequence": {"values": [%q, "SUCCESS"]}}}
		}}
	}`, operation, ack, completion)))
	manifests := []domain.Manifest{{ManifestType: scripted.ManagedManifestType, Raw: raw}}
	return &retryScenario{
		agent: agent, reporter: reporter, observer: observer, firstResult: want,
		send: func(id domain.DeliveryID) error {
			return dispatch(t.Context(), scriptedTarget(), id, manifests, domain.DeliveryAuth{}, nil, 1)
		},
	}
}

func (s *retryScenario) dispatch(t *testing.T, id domain.DeliveryID) {
	t.Helper()
	if err := s.send(id); err != nil {
		t.Fatalf("dispatch %s: %v", id, err)
	}
}

func (s *retryScenario) result(t *testing.T, want domain.DeliveryState) {
	t.Helper()
	if result := <-s.reporter.done; result.State != want {
		t.Fatalf("result = %v, want %v", result.State, want)
	}
}

func (s *retryScenario) holdResult(id domain.DeliveryID) func() {
	gate := make(chan struct{})
	s.reporter.resultGates[id] = gate
	return sync.OnceFunc(func() { close(gate) })
}

// attemptObserver provides independent lifecycle signals for each attempt.
// It preserves the application-owned context used for asynchronous work.
type attemptObserver struct {
	domain.NoOpDeliveryAgentObserver
	attempts map[domain.DeliveryID]*attemptProbe
	enrich   func(context.Context) context.Context
}

func (o *attemptObserver) DeliverStarted(ctx context.Context, id domain.DeliveryID, _ domain.Generation) (context.Context, domain.DeliveryAttemptProbe) {
	if probe := o.attempts[id]; probe != nil {
		if o.enrich != nil {
			ctx = o.enrich(ctx)
		}
		return ctx, probe
	}
	return ctx, domain.NoOpDeliveryAttemptProbe{}
}

func (o *attemptObserver) RemoveStarted(ctx context.Context, id domain.DeliveryID, generation domain.Generation) (context.Context, domain.DeliveryAttemptProbe) {
	return o.DeliverStarted(ctx, id, generation)
}

type attemptProbe struct {
	domain.NoOpDeliveryAttemptProbe
	acknowledged chan struct{}
	ackGate      <-chan struct{}
	ended        chan struct{}
	errors       []error
}

func newAttemptProbe() *attemptProbe {
	return &attemptProbe{acknowledged: make(chan struct{}), ended: make(chan struct{})}
}

func (p *attemptProbe) Acknowledged() {
	close(p.acknowledged)
	if p.ackGate != nil {
		<-p.ackGate
	}
}

func (p *attemptProbe) Error(err error) { p.errors = append(p.errors, err) }

func (p *attemptProbe) End() { close(p.ended) }

func (p *attemptProbe) holdAcknowledgement() func() {
	gate := make(chan struct{})
	p.ackGate = gate
	return sync.OnceFunc(func() { close(gate) })
}

func (p *attemptProbe) waitAcknowledged(t *testing.T) {
	t.Helper()
	waitForAttemptSignal(t, p.acknowledged, "acknowledgement")
}

func (p *attemptProbe) waitEnded(t *testing.T) {
	t.Helper()
	waitForAttemptSignal(t, p.ended, "attempt completion")
}

func waitForAttemptSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	synctest.Wait()
	select {
	case <-signal:
	default:
		t.Fatalf("missing %s signal", name)
	}
}
