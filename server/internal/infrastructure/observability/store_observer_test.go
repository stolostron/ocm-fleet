package observability_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stolostron/ocm-fleet/server/internal/infrastructure/observability"
)

func TestStoreObserverRollbackFailure(testingT *testing.T) {
	for _, test := range []struct {
		name      string
		cancel    bool
		err       error
		wantLevel slog.Level
	}{
		{name: "unexpected", err: errors.New("database unavailable"), wantLevel: slog.LevelError},
		{name: "expected cancellation", cancel: true, err: fmt.Errorf("rollback: %w", context.Canceled), wantLevel: slog.LevelDebug},
		{name: "unrelated failure after cancellation", cancel: true, err: errors.New("database unavailable"), wantLevel: slog.LevelError},
	} {
		testingT.Run(test.name, func(testingT *testing.T) {
			handler := newRecordingHandler(&slog.HandlerOptions{Level: slog.LevelDebug})
			observer := observability.NewStoreObserver(slog.New(handler))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				cancel()
			}
			observer.RollbackFailed(ctx, test.err)
			records := handler.Records()
			if len(records) != 1 || records[0].Level != test.wantLevel {
				testingT.Fatalf("records = %v, want one record at %v", records, test.wantLevel)
			}
			var loggedErr error
			records[0].Attrs(func(attr slog.Attr) bool {
				if attr.Key == "error" {
					loggedErr, _ = attr.Value.Any().(error)
				}
				return true
			})
			if !errors.Is(loggedErr, test.err) {
				testingT.Errorf("logged error = %v, want %v", loggedErr, test.err)
			}
		})
	}
}
