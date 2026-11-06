package domain

import "context"

// DeliveryAgentObserver observes asynchronous delivery-agent attempts.
// Implementations should embed [NoOpDeliveryAgentObserver] for forward
// compatibility. Started methods preserve or enrich the application-owned
// context used for asynchronous work.
type DeliveryAgentObserver interface {
	// DeliverStarted begins observing an accepted delivery attempt.
	DeliverStarted(ctx context.Context, deliveryID DeliveryID, generation Generation) (context.Context, DeliveryAttemptProbe)

	// RemoveStarted begins observing an accepted removal attempt.
	RemoveStarted(ctx context.Context, deliveryID DeliveryID, generation Generation) (context.Context, DeliveryAttemptProbe)
}

// DeliveryAttemptProbe tracks one accepted attempt through result reporting
// and cleanup. Implementations should embed [NoOpDeliveryAttemptProbe] for
// forward compatibility.
type DeliveryAttemptProbe interface {
	// Acknowledged signals that the acknowledgement progress event was reported
	// successfully. A failed acknowledgement does not emit this signal.
	Acknowledged()

	// Error records an error encountered during the attempt.
	Error(err error)

	// End signals that result reporting and attempt cleanup have finished.
	// Called via defer, before the agent's shutdown wait is released.
	End()
}

// NoOpDeliveryAgentObserver returns no-op probes and preserves its input context.
type NoOpDeliveryAgentObserver struct{}

func (NoOpDeliveryAgentObserver) DeliverStarted(ctx context.Context, _ DeliveryID, _ Generation) (context.Context, DeliveryAttemptProbe) {
	return ctx, NoOpDeliveryAttemptProbe{}
}

func (NoOpDeliveryAgentObserver) RemoveStarted(ctx context.Context, _ DeliveryID, _ Generation) (context.Context, DeliveryAttemptProbe) {
	return ctx, NoOpDeliveryAttemptProbe{}
}

// NoOpDeliveryAttemptProbe discards all attempt signals.
type NoOpDeliveryAttemptProbe struct{}

func (NoOpDeliveryAttemptProbe) Acknowledged() {}
func (NoOpDeliveryAttemptProbe) Error(error)   {}
func (NoOpDeliveryAttemptProbe) End()          {}
