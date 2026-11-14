package extensionresourcerepotest

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func runUpdateTests(t *testing.T, factory Factory) {
	ctx := context.Background()
	tx := factory(t)
	defer tx.RollbackOnExit()
	rt := domain.ResourceType("record.example.io/Record")
	seedType(t, tx, rt)
	seedFulfillment(t, tx, "record-update", fixedTime)
	repo := tx.ExtensionResources()
	er := newER(rt, "records/update", "record-update")
	if err := repo.Create(ctx, er); err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyInventoryDeltas(ctx, []domain.InventoryDelta{{ResourceType: rt, Name: er.Name(), CandidateUID: domain.NewExtensionResourceUID(), ReplaceLabels: map[string]string{"provider": "observed"}, ObservedAt: fixedTime, ReceivedAt: fixedTime}}); err != nil {
		t.Fatal(err)
	}
	stale, err := repo.Get(ctx, rt.FullName(er.Name()))
	if err != nil {
		t.Fatal(err)
	}
	er, err = repo.Get(ctx, rt.FullName(er.Name()))
	if err != nil {
		t.Fatal(err)
	}
	intent, err := er.RecordIntent(json.RawMessage(`{"z":"last","provider":"updated","a":"first"}`), fixedTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	er.SetLabels(map[string]string{"user": "updated"}, fixedTime.Add(time.Minute))
	if err := repo.Update(ctx, er); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetView(ctx, rt.FullName(er.Name()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Resource.UID() != er.UID() || got.Intent.Version != 2 {
		t.Fatalf("updated view: %+v", got)
	}
	var values map[string]string
	if err := json.Unmarshal(got.Intent.Spec, &values); err != nil || !maps.Equal(values, map[string]string{"z": "last", "provider": "updated", "a": "first"}) {
		t.Fatalf("updated intent lost values: %s, %v", got.Intent.Spec, err)
	}
	written := got
	written.Intent = &intent
	if written.Etag() != got.Etag() {
		t.Fatal("persisting an immutable intent changed its concurrency token")
	}
	if !maps.Equal(got.Resource.Inventory().Labels(), map[string]string{"provider": "observed"}) || !maps.Equal(got.Resource.Labels(), map[string]string{"user": "updated"}) {
		t.Fatal("managed update changed independent inventory")
	}
	first, err := repo.GetIntent(ctx, er.UID(), 1)
	if err != nil || string(first.Spec) != `{"provider":"kind"}` {
		t.Fatalf("immutable first intent: %+v, %v", first, err)
	}
	if _, err := stale.RecordIntent(json.RawMessage(`{"provider":"stale"}`), fixedTime.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, stale); !errors.Is(err, domain.ErrStaleGeneration) {
		t.Fatalf("stale HEAD must fail CAS: %v", err)
	}
}
