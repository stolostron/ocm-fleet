package scripted_test

import (
	"context"
	"encoding/json"
	"fmt"
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
					firstReport := make(chan struct{})
					defer close(firstReport)
					secondReport := make(chan struct{})
					defer close(secondReport)
					reporter := newChannelReporter()
					reporter.resultGates = map[domain.DeliveryID]<-chan struct{}{
						"d1": firstReport,
						"d2": secondReport,
					}
					agent := newTestAgent(t, reporter, nopInventory{})
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

					if err := dispatch(t.Context(), scriptedTarget(), "d1", manifests, domain.DeliveryAuth{}, nil, 1); err != nil {
						t.Fatalf("first dispatch: %v", err)
					}
					if result := <-reporter.done; result.State != want {
						t.Fatalf("first result = %v, want %v", result.State, want)
					}
					// Keep ReportResult blocked after publishing the terminal result.
					// A platform retry can arrive before that call returns.
					synctest.Wait()
					// Hold the next attempt at its progress event so it stays active
					// while the previous attempt finishes reporting and cleans up.
					reporter.ackCh = make(chan domain.DeliveryEvent)
					if err := dispatch(t.Context(), scriptedTarget(), "d2", manifests, domain.DeliveryAuth{}, nil, 1); err != nil {
						t.Fatalf("dispatch after terminal result: %v", err)
					}
					synctest.Wait()
					firstReport <- struct{}{}
					synctest.Wait()
					if err := dispatch(t.Context(), scriptedTarget(), "d3", manifests, domain.DeliveryAuth{}, nil, 1); err != domain.ErrInvalidArgument {
						t.Fatalf("dispatch while second attempt is active: %v, want ErrInvalidArgument", err)
					}
					<-reporter.ackCh
					if result := <-reporter.done; result.State != domain.DeliveryStateDelivered {
						t.Errorf("second result = %v, want delivered", result.State)
					}

					// Close must still wait for the outstanding result report.
					closed := make(chan error, 1)
					go func() { closed <- agent.Close(context.Background()) }()
					synctest.Wait()
					select {
					case err := <-closed:
						t.Fatalf("Close returned before result reporting finished: %v", err)
					default:
					}
					secondReport <- struct{}{}
					if err := <-closed; err != nil {
						t.Fatalf("Close: %v", err)
					}
				})
			})
		}
	}
}
