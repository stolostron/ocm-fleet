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

func TestManagedResourceUpdate_FenceSerializesWithConditionalSave(t *testing.T) {
	for _, fenceFirst := range []bool{true, false} {
		name := "save commits first"
		if fenceFirst {
			name = "fence commits first"
		}
		t.Run(name, func(t *testing.T) {
			store := newStore(t)
			db := store.DB
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
			typeDef := domain.NewExtensionResourceType("records.example.io/Record", "v1alpha", "records", now,
				domain.WithManagement(domain.NewRegisteredSelfTarget("record-local", "managed.record"), domain.Signature{}))
			seed, err := store.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer seed.RollbackOnExit()
			if err := seed.ExtensionResources().CreateType(ctx, typeDef); err != nil {
				t.Fatal(err)
			}
			if err := seed.Commit(); err != nil {
				t.Fatal(err)
			}
			create := &domain.CreateManagedResourceWorkflowSpec{Store: store}
			initial, err := create.PersistManagedResource().Run(ctx, domain.CreateManagedResourceInput{
				RequestID: "create-fence-race", ResourceType: typeDef.ResourceType(), Name: "records/fence-race", TypeDef: typeDef, Spec: []byte(`{}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			mutation, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer sqltxtest.Rollback(t, mutation)
			fence, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer sqltxtest.Rollback(t, fence)
			mutationRepo := &postgres.ExtensionResourceRepo{DB: mutation}
			fenceRepo := &postgres.ExtensionResourceRepo{DB: fence}
			// Capture an old snapshot before either write. The conditional
			// SQL must arbitrate correctly even when no root read lock is
			// held; recovery's fence needs no such lock or preliminary read.
			er, err := mutationRepo.Get(ctx, typeDef.ResourceType().FullName(initial.Resource.Name()))
			if err != nil {
				t.Fatal(err)
			}
			expectedRevision := er.Revision()
			if _, err := er.RecordIntent([]byte(`{"changed":true}`), now); err != nil {
				t.Fatal(err)
			}
			var blockedPID int
			done := make(chan error, 1)
			if fenceFirst {
				if err := fenceRepo.FenceUpdate(ctx, er.UID(), expectedRevision); err != nil {
					t.Fatal(err)
				}
				if err := mutation.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&blockedPID); err != nil {
					t.Fatal(err)
				}
				go func() { done <- mutationRepo.Update(ctx, er) }()
				waitForBlockedBackend(t, db, blockedPID)
				if err := fence.Commit(); err != nil {
					t.Fatal(err)
				}
				if err := <-done; !errors.Is(err, domain.ErrStaleGeneration) {
					t.Fatalf("late save escaped a committed fence: %v", err)
				}
				if err := mutation.Rollback(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := mutationRepo.Update(ctx, er); err != nil {
					t.Fatal(err)
				}
				if err := fence.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&blockedPID); err != nil {
					t.Fatal(err)
				}
				go func() { done <- fenceRepo.FenceUpdate(ctx, er.UID(), expectedRevision) }()
				waitForBlockedBackend(t, db, blockedPID)
				if err := mutation.Commit(); err != nil {
					t.Fatal(err)
				}
				if err := <-done; err != nil {
					t.Fatalf("fence of an already committed save failed: %v", err)
				}
				if err := fence.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			read, err := store.BeginReadOnly(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer read.RollbackOnExit()
			current, err := read.ExtensionResources().GetView(ctx, typeDef.ResourceType().FullName(initial.Resource.Name()))
			if err != nil {
				t.Fatal(err)
			}
			wantVersion := domain.IntentVersion(2)
			if fenceFirst {
				wantVersion = 1
			}
			if current.Resource.Revision() != expectedRevision+1 || current.Intent.Version != wantVersion {
				t.Fatalf("unexpected committed result: revision %d, intent %d", current.Resource.Revision(), current.Intent.Version)
			}
		})
	}
}
