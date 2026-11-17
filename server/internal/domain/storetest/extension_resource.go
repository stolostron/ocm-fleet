package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func runExtensionResourceCreationTests(t *testing.T, factory Factory) {
	for _, operation := range []string{"accept", "reject", "already-decided"} {
		for _, commit := range []bool{false, true} {
			outcome := "rollback"
			if commit {
				outcome = "commit"
			}
			t.Run(operation+"/"+outcome, func(t *testing.T) {
				ctx := context.Background()
				now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
				store := factory(t)
				begin := func() domain.Tx {
					t.Helper()
					tx, err := store.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(tx.RollbackOnExit)
					return tx
				}
				tx := begin()
				rt := domain.ResourceType("records.example.io/Record")
				if err := tx.ExtensionResources().CreateType(ctx, domain.NewExtensionResourceType(rt, "v1", "records", now, domain.WithInventory())); err != nil {
					t.Fatal(err)
				}
				// An inventory-only occupant must also permit recording a
				// rejected managed Create; no fulfillment or intent is required.
				original := domain.NewExtensionResource(domain.NewExtensionResourceUID(), rt, "records/occupied", "original", nil, now)
				if result, err := tx.ExtensionResources().Create(ctx, original); err != nil || result != domain.ExtensionResourceCreated {
					t.Fatalf("seed occupant: %v, %v", result, err)
				}
				if err := tx.ExtensionResources().ApplyInventoryDeltas(ctx, []domain.InventoryDelta{{
					ResourceType: rt, Name: original.Name(), CandidateUID: domain.NewExtensionResourceUID(),
					ReplaceLabels: map[string]string{"provider": "observed"}, ObservedAt: now, ReceivedAt: now,
				}}); err != nil {
					t.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}

				tx = begin()
				name, nonce := domain.ResourceName("records/new"), "new-request"
				want := domain.ExtensionResourceCreated
				if operation == "reject" {
					name, want = original.Name(), domain.ExtensionResourceCreateRejected
				} else if operation == "already-decided" {
					nonce, want = "original", domain.ExtensionResourceCreateAlreadyDecided
				}
				candidate := domain.NewExtensionResource(domain.NewExtensionResourceUID(), rt, name, nonce, nil, now,
					domain.WithManagedState("attempt"), domain.WithExtensionLabels(map[string]string{"attempt": "new"}))
				if _, err := candidate.RecordIntent([]byte(`{}`), now); err != nil {
					t.Fatal(err)
				}
				result, err := tx.ExtensionResources().Create(ctx, candidate)
				if err != nil || result != want {
					t.Fatalf("Create: %v, %v, want %v", result, err, want)
				}
				if commit {
					if err := tx.Commit(); err != nil {
						t.Fatal(err)
					}
				} else if err := tx.Rollback(); err != nil {
					t.Fatal(err)
				}

				tx = begin()
				repo := tx.ExtensionResources()
				created, err := repo.Get(ctx, rt.FullName("records/new"))
				if commit && operation == "accept" {
					if err != nil || created.UID() != candidate.UID() {
						t.Fatalf("committed acceptance lost its resource: %v", err)
					}
				} else if !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("unsaved attempt left a provisional resource: %v", err)
				}
				occupant, err := repo.Get(ctx, rt.FullName(original.Name()))
				if err != nil {
					t.Fatal(err)
				}
				if occupant.UID() != original.UID() || len(occupant.Labels()) != 0 || !occupant.UpdatedAt().Equal(now) {
					t.Fatal("creation attempt changed the occupying resource")
				}
				if occupant.Inventory() == nil || occupant.Inventory().Labels()["provider"] != "observed" {
					t.Fatal("creation attempt changed independently reported inventory")
				}
				decision, err := repo.GetCreation(ctx, "new-request")
				if commit && operation != "already-decided" {
					if err != nil || !decision.MatchesScope(rt, name, nil) || decision.AlreadyExists() != (operation == "reject") {
						t.Fatalf("committed decision: %+v, %v", decision.Snapshot(), err)
					}
				} else if !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("unsaved attempt left a creation decision: %v", err)
				}
				decision, err = repo.GetCreation(ctx, "original")
				if err != nil || decision.UID() != original.UID() || !decision.MatchesScope(rt, original.Name(), nil) {
					t.Fatalf("original creation decision changed: %+v, %v", decision.Snapshot(), err)
				}
				_, err = repo.GetIntent(ctx, candidate.UID(), 1)
				if commit && operation == "accept" {
					if err != nil {
						t.Fatal(err)
					}
				} else if !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("unsaved attempt left a resource intent: %v", err)
				}
			})
		}
	}
}
