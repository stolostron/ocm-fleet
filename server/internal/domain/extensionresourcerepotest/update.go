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
	if _, err := repo.Create(ctx, er); err != nil {
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
	loadedRevision := er.Revision()
	intent, err := er.RecordIntent(json.RawMessage(`{"z":"last","provider":"updated","a":"first"}`), fixedTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	er.SetLabels(map[string]string{"user": "updated"}, fixedTime.Add(time.Minute))
	if er.Revision() != loadedRevision+1 {
		t.Fatal("the aggregate must advance its revision before saving")
	}
	if err := repo.Update(ctx, er); err != nil {
		t.Fatal(err)
	}
	if er.Revision() != loadedRevision+1 {
		t.Fatal("the repository must preserve the aggregate's revision")
	}
	got, err := repo.GetView(ctx, rt.FullName(er.Name()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Resource.UID() != er.UID() || got.Intent.Version != 2 {
		t.Fatalf("updated view: %+v", got)
	}
	if got.Resource.Revision() != loadedRevision+1 {
		t.Fatal("update revision was lost in persistence or view hydration")
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

func runUpdateLabelsWithoutManagedStateTests(t *testing.T, factory Factory) {
	ctx := context.Background()
	tx := factory(t)
	defer tx.RollbackOnExit()
	repo := tx.ExtensionResources()
	rt := domain.ResourceType("inventory.example.io/Object")
	if err := repo.CreateType(ctx, domain.NewExtensionResourceType(rt, "v1", "objects", fixedTime, domain.WithInventory())); err != nil {
		t.Fatal(err)
	}
	er := domain.NewExtensionResource(domain.NewExtensionResourceUID(), rt, "objects/labels", "create-labels", nil, fixedTime,
		domain.WithExtensionLabels(map[string]string{"phase": "original"}))
	if _, err := repo.Create(ctx, er); err != nil {
		t.Fatal(err)
	}
	initial, err := repo.GetView(ctx, er.FullResourceName())
	if err != nil {
		t.Fatal(err)
	}
	stale, err := repo.Get(ctx, er.FullResourceName())
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"temporary", "original"} {
		er.SetLabels(map[string]string{"phase": phase}, fixedTime)
		if err := repo.Update(ctx, er); err != nil {
			t.Fatalf("label update requires no managed state: %v", err)
		}
	}
	current, err := repo.GetView(ctx, er.FullResourceName())
	if err != nil {
		t.Fatal(err)
	}
	if current.Resource.Managed() != nil || current.Intent != nil || current.Fulfillment != nil {
		t.Fatal("label updates introduced managed state")
	}
	if current.Resource.Revision() != initial.Resource.Revision()+2 {
		t.Fatal("label updates did not persist the root revision")
	}
	if current.Etag() == initial.Etag() {
		t.Error("restoring labels revived an old etag on an ER without managed state")
	}
	stale.SetLabels(map[string]string{"phase": "stale writer"}, fixedTime)
	if err := repo.Update(ctx, stale); !errors.Is(err, domain.ErrStaleGeneration) {
		t.Fatalf("a stale label save must fail even without managed state: %v", err)
	}
}

func runUpdateFenceWithoutManagedStateTests(t *testing.T, factory Factory) {
	ctx := context.Background()
	tx := factory(t)
	defer tx.RollbackOnExit()
	repo := tx.ExtensionResources()
	rt := domain.ResourceType("inventory.example.io/Object")
	if err := repo.CreateType(ctx, domain.NewExtensionResourceType(rt, "v1", "objects", fixedTime, domain.WithInventory())); err != nil {
		t.Fatal(err)
	}
	er := domain.NewExtensionResource(domain.NewExtensionResourceUID(), rt, "objects/fence", "create-fence", nil, fixedTime)
	if _, err := repo.Create(ctx, er); err != nil {
		t.Fatal(err)
	}
	initial, err := repo.GetView(ctx, er.FullResourceName())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repo.FenceUpdate(ctx, er.UID(), 0); err != nil {
			t.Fatal(err)
		}
	}
	current, err := repo.GetView(ctx, er.FullResourceName())
	if err != nil {
		t.Fatal(err)
	}
	if current.Etag() == initial.Etag() {
		t.Error("retiring an unmanaged ER's revision must invalidate its old etag")
	}
	if current.Resource.Managed() != nil || !current.Resource.UpdatedAt().Equal(fixedTime) || len(current.Resource.Labels()) != 0 {
		t.Fatal("fencing changed user state or introduced managed state")
	}
	if current.Resource.Revision() != 1 {
		t.Fatal("the fence must retire the root revision once")
	}
	byUID, err := repo.GetByUID(ctx, er.UID())
	if err != nil || byUID.Revision() != current.Resource.Revision() {
		t.Fatalf("UID projection lost an unmanaged ER's revision: %v", err)
	}
	listed, err := repo.ListByResourceType(ctx, rt)
	if err != nil || len(listed) != 1 || listed[0].Revision() != current.Resource.Revision() {
		t.Fatalf("list projection lost an unmanaged ER's revision: %v", err)
	}
	page, err := tx.Queries().QueryResources(ctx, domain.QueryResourcesRequest{})
	if err != nil || len(page.Resources) != 1 || page.Resources[0].Extension == nil || page.Resources[0].Extension.Resource.Revision() != current.Resource.Revision() {
		t.Fatalf("query projection lost an unmanaged ER's revision: %v", err)
	}
	er.SetLabels(map[string]string{"phase": "late writer"}, fixedTime)
	if err := repo.Update(ctx, er); !errors.Is(err, domain.ErrStaleGeneration) {
		t.Fatalf("a delayed label save escaped the fence: %v", err)
	}
}

func runUpdateRepeatedSavesTests(t *testing.T, factory Factory) {
	ctx := context.Background()
	tx := factory(t)
	defer tx.RollbackOnExit()
	rt := domain.ResourceType("record.example.io/Record")
	seedType(t, tx, rt)
	seedFulfillment(t, tx, "record-repeated-saves", fixedTime)
	repo := tx.ExtensionResources()
	er := newER(rt, "records/repeated-saves", "record-repeated-saves")
	if _, err := repo.Create(ctx, er); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []json.RawMessage{json.RawMessage(`{"version":2}`), json.RawMessage(`{"version":3}`)} {
		loadedRevision := er.Revision()
		if _, err := er.RecordIntent(spec, fixedTime); err != nil {
			t.Fatal(err)
		}
		er.SetLabels(map[string]string{"user": "updated"}, fixedTime)
		beforeSave := domain.ExtensionResourceView{Resource: *er}
		if er.Revision() != loadedRevision+1 {
			t.Fatal("the previous save did not establish the next mutation's baseline")
		}
		etag := beforeSave.Etag()
		if err := repo.Update(ctx, er); err != nil {
			t.Fatalf("repeated aggregate save failed: %v", err)
		}
		if (domain.ExtensionResourceView{Resource: *er}).Etag() != etag || len(er.Snapshot().PendingIntents) != 0 {
			t.Fatal("saving changed the etag or left already-persisted intents pending")
		}
		if err := repo.Update(ctx, er); err != nil {
			t.Fatalf("saving an unchanged aggregate must not replay pending writes: %v", err)
		}
		if (domain.ExtensionResourceView{Resource: *er}).Etag() != etag {
			t.Fatal("saving an unchanged aggregate advanced its revision")
		}
	}
}

func runUpdateFenceTests(t *testing.T, factory Factory) {
	ctx := context.Background()
	tx := factory(t)
	defer tx.RollbackOnExit()
	rt := domain.ResourceType("record.example.io/Record")
	seedType(t, tx, rt)
	seedFulfillment(t, tx, "record-fence", fixedTime)
	repo := tx.ExtensionResources()
	er := newER(rt, "records/fence", "record-fence")
	if _, err := repo.Create(ctx, er); err != nil {
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
	loadedRevision := er.Revision()
	// Retiring a revision and retrying its fence must be one atomic decision,
	// independent of intent versions and without an aggregate mutation.
	for offset := range domain.ExtensionResourceRevision(2) {
		revision := loadedRevision + offset
		if err := repo.FenceUpdate(ctx, er.UID(), revision); err != nil {
			t.Fatal(err)
		}
		if err := repo.FenceUpdate(ctx, er.UID(), revision); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.FenceUpdate(ctx, domain.NewExtensionResourceUID(), 0); err != nil {
		t.Fatalf("fencing an absent incarnation must be a no-op: %v", err)
	}
	view, err := repo.GetView(ctx, rt.FullName(er.Name()))
	if err != nil {
		t.Fatal(err)
	}
	if view.Resource.Revision() != loadedRevision+2 || view.Intent.Version != 1 || !view.Resource.UpdatedAt().Equal(fixedTime) {
		t.Fatal("fence-only saves changed desired intent or failed to persist revision")
	}
	byUID, err := repo.GetByUID(ctx, er.UID())
	if err != nil || byUID.Revision() != loadedRevision+2 {
		t.Fatalf("UID projection lost the revision: %v", err)
	}
	listed, err := repo.ListByResourceType(ctx, rt)
	if err != nil || len(listed) != 1 || listed[0].Revision() != loadedRevision+2 {
		t.Fatalf("list projection lost the revision: %v", err)
	}
	// Even without an intent-version change, a stale aggregate cannot
	// overwrite the new revision. Callers roll back a failed save.
	if err := repo.Update(ctx, stale); !errors.Is(err, domain.ErrStaleGeneration) {
		t.Fatalf("stale revision passed compare-and-swap: %v", err)
	}
}
