package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func TestStore_FulfillmentWritersReadAfterPriorCommit(t *testing.T) {
	db := OpenTestDB(t)
	store := &Store{DB: db}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	seed, err := store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer seed.RollbackOnExit()
	f := domain.NewFulfillment("concurrent-managed-update", domain.DeliveryAuth{}, nil, nil, now)
	f.AdvanceManifestStrategy(domain.ManifestStrategySpec{Type: domain.ManifestStrategyInline, Manifests: []domain.Manifest{{Raw: json.RawMessage(`{"version":1}`)}}}, now)
	f.AdvancePlacementStrategy(domain.PlacementStrategySpec{Type: domain.PlacementStrategyAll}, now)
	if err := seed.Fulfillments().Create(ctx, f); err != nil {
		t.Fatal(err)
	}
	if err := seed.Commit(); err != nil {
		t.Fatal(err)
	}
	a, err := store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer a.RollbackOnExit()
	f, err = a.Fulfillments().Get(ctx, f.ID())
	if err != nil {
		t.Fatal(err)
	}
	f.AdvanceManifestStrategy(domain.ManifestStrategySpec{Type: domain.ManifestStrategyInline, Manifests: []domain.Manifest{{Raw: json.RawMessage(`{"version":2}`)}}}, now.Add(time.Minute))
	if err := a.Fulfillments().Update(ctx, f); err != nil {
		t.Fatal(err)
	}
	b, err := store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer b.RollbackOnExit()
	var pid int
	if err := b.(*storeTx).tx.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	type loaded struct {
		fulfillment *domain.Fulfillment
		err         error
	}
	done := make(chan loaded, 1)
	go func() { f, err := b.Fulfillments().Get(ctx, "concurrent-managed-update"); done <- loaded{f, err} }()
	// Real PostgreSQL lock state establishes the interleaving. Repeated I/O
	// queries, bounded by ctx, avoid timing assumptions or sleeps in the test.
	for {
		var blocked bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
	}
	if err := a.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		f = got.fulfillment
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var manifest struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(f.ManifestStrategy().Manifests[0].Raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if f.Generation() != 2 || f.ManifestStrategyVersion() != 2 || manifest.Version != 2 {
		t.Fatalf("writer read stale aggregate or strategy after waiting: %+v", f.Snapshot())
	}
	f.Touch(now.Add(2 * time.Minute))
	if err := b.Fulfillments().Update(ctx, f); err != nil {
		t.Fatal(err)
	}
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
	if f.Generation() != 3 {
		t.Fatalf("lost concurrent generation: %d", f.Generation())
	}
}
