package record

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

// Agent acknowledges record intent within the resource manager's authenticated
// process boundary. It performs no external action and starts no background
// work: replaying a dispatch after a crash simply validates and reports again.
// Detached signatures are verified by the managed-resource application service.
type Agent struct {
	reporter domain.DeliveryReporter
}

// NewAgent constructs the synchronous record provider.
func NewAgent(reporter domain.DeliveryReporter) *Agent { return &Agent{reporter: reporter} }

// Deliver accepts a validated record. All outcomes pass through the reporter.
func (a *Agent) Deliver(ctx context.Context, target domain.TargetInfo, id domain.DeliveryID, manifests []domain.Manifest, _ domain.DeliveryAuth, _ *domain.Attestation, generation domain.Generation) error {
	return a.report(ctx, target, id, manifests, generation)
}

// Remove acknowledges removal of record intent; there is no external state.
func (a *Agent) Remove(ctx context.Context, target domain.TargetInfo, id domain.DeliveryID, manifests []domain.Manifest, _ domain.DeliveryAuth, _ *domain.Attestation, generation domain.Generation) error {
	return a.report(ctx, target, id, manifests, generation)
}

func (a *Agent) report(ctx context.Context, target domain.TargetInfo, id domain.DeliveryID, manifests []domain.Manifest, generation domain.Generation) error {
	result := domain.DeliveryResult{State: domain.DeliveryStateDelivered}
	if err := validate(target, manifests); err != nil {
		result.State, result.Message = domain.DeliveryStateFailed, err.Error()
	}
	return a.reporter.ReportResult(ctx, id, generation, result)
}

func validate(target domain.TargetInfo, manifests []domain.Manifest) error {
	if target.ID() != TargetID || target.Type() != TargetType || len(manifests) != 1 || manifests[0].ManifestType != ManifestType {
		return fmt.Errorf("invalid record target or manifest")
	}
	envelope, err := domain.UnwrapManagedResourceSpec(manifests[0].Raw)
	if err != nil {
		return err
	}
	if _, err := domain.NewResourceName("records", envelope.Name.ID()); err != nil || envelope.Name != domain.ResourceName("records/"+string(envelope.Name.ID())) {
		return fmt.Errorf("invalid record resource name %q", envelope.Name)
	}
	var spec struct {
		Values map[string]string `json:"values"`
	}
	dec := json.NewDecoder(bytes.NewReader(envelope.Spec))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("invalid trailing record content")
	}
	if len(spec.Values) > 32 {
		return fmt.Errorf("record exceeds 32 values")
	}
	for key, value := range spec.Values {
		if n := utf8.RuneCountInString(key); n < 1 || n > 128 || utf8.RuneCountInString(value) > 1024 {
			return fmt.Errorf("record key or value exceeds schema bounds")
		}
	}
	return nil
}
