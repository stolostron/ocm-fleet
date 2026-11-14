package fleet_test

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stolostron/ocm-fleet/controllers/record/fleet"
	"github.com/stolostron/ocm-fleet/server/testserver"
)

func TestClient_RealFleetLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	remote, err := fleet.NewClient(testserver.StartRecords(t), &http.Client{Timeout: 10 * time.Second}, func(context.Context) (string, error) { return "test-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	name := "records/client-lifecycle"
	want := fleet.Spec{Values: map[string]string{"message": "hello"}}
	requestID := uuid.NewString()
	created, err := remote.Create(ctx, name, requestID, want)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := remote.Create(ctx, name, requestID, want)
	if err != nil || replayed.UID != created.UID {
		t.Fatalf("create replay: %+v, %v", replayed, err)
	}
	current := awaitRecord(t, ctx, remote, name, func(r fleet.Record) bool { return r.Ready() })
	changed := fleet.Spec{Values: map[string]string{"message": "updated"}}
	updated, err := remote.Update(ctx, current, changed)
	if err != nil {
		t.Fatal(err)
	}
	if updated.UID != created.UID || updated.Generation != 2 || !maps.Equal(updated.Spec.Values, changed.Values) {
		t.Fatalf("update: %+v", updated)
	}
	if _, err := remote.Update(ctx, current, want); !fleet.IsConflict(err) {
		t.Fatalf("stale update: %v", err)
	}
	if err := remote.Delete(ctx, current); !fleet.IsConflict(err) {
		t.Fatalf("stale delete: %v", err)
	}
	current = awaitRecord(t, ctx, remote, name, func(r fleet.Record) bool { return r.Ready() && r.Generation == 2 })
	if err := remote.Delete(ctx, current); err != nil {
		t.Fatal(err)
	}
	for {
		_, err := remote.Get(ctx, name)
		if fleet.IsNotFound(err) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := remote.Create(ctx, name, requestID, want); !fleet.IsCreationRetired(err) {
		t.Fatalf("retired Create must return a definitive creation decision: %v", err)
	}
	if _, err := remote.Get(ctx, name); !fleet.IsNotFound(err) {
		t.Fatalf("old create resurrected the resource: %v", err)
	}
	_, err = remote.Create(ctx, name, requestID, changed)
	var apiErr *fleet.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("request ID reuse: %v", err)
	}
}

// Network requests yield to the real workflow worker. The deadline bounds an
// assertion about asynchronous I/O; no elapsed-time assumption drives it.
func awaitRecord(t *testing.T, ctx context.Context, remote *fleet.Client, name string, predicate func(fleet.Record) bool) fleet.Record {
	t.Helper()
	for {
		current, err := remote.Get(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if predicate(current) {
			return current
		}
	}
}
