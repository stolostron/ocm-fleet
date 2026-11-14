package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/application"
)

// startManagedResourceMaintenance runs once at startup and periodically after
// that. Current database state is the recovery cursor, so process failure and
// overlapping server instances cannot lose a generation or cleanup obligation.
func startManagedResourceMaintenance(ctx context.Context, maintenance *application.ManagedResourceMaintenance, logger *slog.Logger) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			if err := maintenance.RunOnce(ctx, time.Now()); err != nil && ctx.Err() == nil {
				logger.Error("managed resource maintenance failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}
