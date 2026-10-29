package observability

import (
	"context"
	"errors"
	"log/slog"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

// StoreObserver logs transaction cleanup failures.
type StoreObserver struct {
	domain.NoOpStoreObserver
	logger *slog.Logger
}

// NewStoreObserver returns an observer that logs transaction cleanup failures.
func NewStoreObserver(logger *slog.Logger) *StoreObserver {
	return &StoreObserver{logger: logger.With("component", "store")}
}

func (observer *StoreObserver) RollbackFailed(ctx context.Context, err error) {
	level := slog.LevelError
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		level = slog.LevelDebug
	}
	observer.logger.LogAttrs(ctx, level, "transaction rollback failed", slog.Any("error", err))
}
