package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
	"github.com/stolostron/ocm-fleet/server/internal/infrastructure/postgres"
	"github.com/stolostron/ocm-fleet/server/internal/infrastructure/sqltxtest"
)

func TestExtensionResourceCreate_ConcurrentDecisions(t *testing.T) {
	for _, tc := range []struct {
		name          string
		duplicate     bool
		differentName bool
		rollback      bool
		rejected      bool
	}{
		{name: "competing-nonces"},
		{name: "duplicate-nonce", duplicate: true},
		{name: "nonce-reused-for-another-name", duplicate: true, differentName: true},
		{name: "rollback-competing-nonces", rollback: true},
		{name: "rollback-duplicate-nonce", duplicate: true, rollback: true},
		{name: "rejected-nonce-after-deletion", duplicate: true, rejected: true},
		{name: "rolled-back-rejection-after-deletion", duplicate: true, rejected: true, rollback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := postgres.OpenTestDB(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			rt := domain.ResourceType("records.example.io/Record")
			resourceName := domain.ResourceName("records/concurrent")
			now := time.Unix(0, 0)
			seed, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer sqltxtest.Rollback(t, seed)
			seedRepo := &postgres.ExtensionResourceRepo{DB: seed}
			if err := seedRepo.CreateType(ctx, domain.NewExtensionResourceType(rt, "v1", "records", now)); err != nil {
				t.Fatal(err)
			}
			if tc.rejected {
				occupant := domain.NewExtensionResource(domain.NewExtensionResourceUID(), rt, resourceName, "N1", nil, now)
				if result, err := seedRepo.Create(ctx, occupant); err != nil || result != domain.ExtensionResourceCreated {
					t.Fatalf("seed occupant: %v, %v", result, err)
				}
			}
			if err := seed.Commit(); err != nil {
				t.Fatal(err)
			}

			txA, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer sqltxtest.Rollback(t, txA)
			txB, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer sqltxtest.Rollback(t, txB)
			var pidB int
			if err := txB.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pidB); err != nil {
				t.Fatal(err)
			}
			repoA := &postgres.ExtensionResourceRepo{DB: txA}
			repoB := &postgres.ExtensionResourceRepo{DB: txB}
			a := domain.NewExtensionResource(domain.NewExtensionResourceUID(), rt, resourceName, "N2", nil, now)
			wantA := domain.ExtensionResourceCreated
			if tc.rejected {
				wantA = domain.ExtensionResourceCreateRejected
			}
			if result, err := repoA.Create(ctx, a); err != nil || result != wantA {
				t.Fatalf("first attempt: %v, %v, want %v", result, err, wantA)
			}
			if tc.rejected {
				// The name can disappear after we observed its conflict but
				// before the rejection commits. A duplicate can then insert a
				// provisional resource; request-ID uniqueness must still fence it.
				if _, err := db.ExecContext(ctx, `DELETE FROM extension_resources WHERE service_name = $1 AND collection_name = 'records' AND resource_id = 'concurrent'`, string(rt.ServiceName())); err != nil {
					t.Fatal(err)
				}
			}
			requestB, nameB := "N3", resourceName
			if tc.duplicate {
				requestB = "N2"
			}
			if tc.differentName {
				nameB = "records/another"
			}
			b := domain.NewExtensionResource(domain.NewExtensionResourceUID(), rt, nameB, requestB, nil, now)
			type result struct {
				outcome domain.ExtensionResourceCreateResult
				err     error
			}
			done := make(chan result, 1)
			go func() {
				outcome, err := repoB.Create(ctx, b)
				done <- result{outcome: outcome, err: err}
			}()
			// Observe an actual database lock wait, rather than assuming the
			// goroutine reached its insert after some elapsed time. These are
			// native uniqueness waits; Create takes no advisory or read locks.
			waitForBlockedBackend(t, db, pidB)
			if tc.rollback {
				err = txA.Rollback()
			} else {
				err = txA.Commit()
			}
			if err != nil {
				t.Fatal(err)
			}
			wantB := domain.ExtensionResourceCreateRejected
			if tc.rollback {
				wantB = domain.ExtensionResourceCreated
			} else if tc.duplicate {
				wantB = domain.ExtensionResourceCreateAlreadyDecided
			}
			select {
			case got := <-done:
				if got.err != nil || got.outcome != wantB {
					t.Fatalf("concurrent attempt: %v, %v, want %v", got.outcome, got.err, wantB)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// Even committing an already-decided result must not leave the
			// provisional resource behind; cleanup belongs to the repository.
			if err := txB.Commit(); err != nil {
				t.Fatal(err)
			}
			reader := &postgres.ExtensionResourceRepo{DB: db}
			decision, err := reader.GetCreation(ctx, requestB)
			if err != nil {
				t.Fatal(err)
			}
			wantUID := domain.ExtensionResourceUID{}
			if tc.rollback {
				wantUID = b.UID()
			} else if tc.duplicate && !tc.rejected {
				wantUID = a.UID()
			}
			if decision.UID() != wantUID {
				t.Fatalf("winning decision UID: %v, want %v", decision.UID(), wantUID)
			}
			if wantB != domain.ExtensionResourceCreated {
				if _, err := reader.GetByUID(ctx, b.UID()); !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("losing attempt left a resource: %v", err)
				}
			}
			var count int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM extension_resources`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			wantCount := 1
			if tc.rejected && !tc.rollback {
				wantCount = 0
			}
			if count != wantCount {
				t.Fatalf("committed resources: %d, want %d", count, wantCount)
			}
		})
	}
}
