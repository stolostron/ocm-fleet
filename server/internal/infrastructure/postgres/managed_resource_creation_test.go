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

func TestCreationDecision_SerializesMissingNamesAndDuplicateRequests(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		name := "competing-nonces"
		if duplicate {
			name = "duplicate-nonce"
		}
		t.Run(name, func(t *testing.T) {
			db := postgres.OpenTestDB(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
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
			resourceName := domain.ResourceType("records.example.io/Record").FullName("records/concurrent")
			repoA := &postgres.ExtensionResourceRepo{DB: txA}
			repoB := &postgres.ExtensionResourceRepo{DB: txB}
			if _, err := repoA.GetCreation(ctx, "N2", resourceName); !errors.Is(err, domain.ErrNotFound) {
				t.Fatal(err)
			}
			requestB := "N3"
			if duplicate {
				requestB = "N2"
			}
			done := make(chan error, 1)
			go func() {
				decision, err := repoB.GetCreation(ctx, requestB, resourceName)
				if duplicate && err == nil && decision.AlreadyExists() {
					done <- nil
					return
				}
				if !duplicate && errors.Is(err, domain.ErrNotFound) {
					done <- nil
					return
				}
				if err == nil {
					err = errors.New("duplicate did not observe the committed decision")
				}
				done <- err
			}()
			// Observe an actual database lock wait, rather than assuming the
			// goroutine reached its lookup after some elapsed time.
			waitForBlockedBackend(t, db, pidB)
			if err := repoA.PutCreation(ctx, domain.NewManagedResourceCreation("N2", "fingerprint", domain.ExtensionResourceUID{}, time.Unix(1_800_000_000, 0))); err != nil {
				t.Fatal(err)
			}
			if err := txA.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
