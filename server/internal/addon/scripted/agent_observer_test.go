package scripted_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/stolostron/ocm-fleet/server/internal/addon/scripted"
	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func TestAgent_ObserverContextAndReportError(t *testing.T) {
	for _, operation := range []string{"delivery", "removal"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				probe := newAttemptProbe()
				observer := &attemptObserver{
					attempts: map[domain.DeliveryID]*attemptProbe{"first": probe},
					enrich: func(ctx context.Context) context.Context {
						return context.WithValue(ctx, attemptContextKey{}, "first")
					},
				}
				reportErr := errors.New("result report failed")
				reporter := &contextErrorReporter{DeliveryReporter: newChannelReporter(), err: reportErr}
				agent := newTestAgent(t, reporter, nopInventory{}, scripted.WithObserver(observer))
				dispatch := agent.Deliver
				if operation == "removal" {
					dispatch = agent.Remove
				}
				_, raw := wrapSpec(t, json.RawMessage(`{}`))
				manifests := []domain.Manifest{{ManifestType: scripted.ManagedManifestType, Raw: raw}}
				if err := dispatch(t.Context(), scriptedTarget(), "first", manifests, domain.DeliveryAuth{}, nil, 1); err != nil {
					t.Fatalf("dispatch: %v", err)
				}
				probe.waitEnded(t)
				if len(probe.errors) != 1 || !errors.Is(probe.errors[0], reportErr) {
					t.Fatalf("probe errors = %v, want result reporting error", probe.errors)
				}
			})
		})
	}
}

type attemptContextKey struct{}

// contextErrorReporter requires the observer-enriched context and simulates a
// failed terminal report after a successfully reported acknowledgement.
type contextErrorReporter struct {
	domain.DeliveryReporter
	err error
}

func (r *contextErrorReporter) ReportEvent(ctx context.Context, id domain.DeliveryID, generation domain.Generation, event domain.DeliveryEvent) error {
	if ctx.Value(attemptContextKey{}) != "first" {
		return errors.New("event report lost observer context")
	}
	return r.DeliveryReporter.ReportEvent(ctx, id, generation, event)
}

func (r *contextErrorReporter) ReportResult(ctx context.Context, _ domain.DeliveryID, _ domain.Generation, _ domain.DeliveryResult) error {
	if ctx.Value(attemptContextKey{}) != "first" {
		return errors.New("result report lost observer context")
	}
	return r.err
}
