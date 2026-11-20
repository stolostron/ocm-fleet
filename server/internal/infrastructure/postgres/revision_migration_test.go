package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
	"github.com/stolostron/ocm-fleet/server/internal/infrastructure/postgres"
)

func TestExtensionResourceRevisionMigration_PreservesExistingFence(t *testing.T) {
	store := &postgres.Store{DB: postgres.OpenTestDB(t)}
	ctx := context.Background()
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	typeDef := domain.NewExtensionResourceType("records.example.io/Record", "v1alpha", "records", now,
		domain.WithManagement(domain.NewRegisteredSelfTarget("record-local", "managed.record"), domain.Signature{}))
	tx, err := store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	if err := tx.ExtensionResources().CreateType(ctx, typeDef); err != nil {
		t.Fatal(err)
	}
	unmanaged := domain.NewExtensionResource(domain.NewExtensionResourceUID(), typeDef.ResourceType(), "records/unmanaged", "create-unmanaged", nil, now,
		domain.WithExtensionLabels(map[string]string{"owner": "user"}))
	if _, err := tx.ExtensionResources().Create(ctx, unmanaged); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Now: func() time.Time { return now }}
	initial, err := create.PersistManagedResource().Run(ctx, domain.CreateManagedResourceInput{
		RequestID: "create-migration", ResourceType: typeDef.ResourceType(), Name: "records/migration", TypeDef: typeDef, Spec: []byte(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err = store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	for offset := range domain.ExtensionResourceRevision(2) {
		if err := tx.ExtensionResources().FenceUpdate(ctx, initial.Resource.UID(), initial.Resource.Revision()+offset); err != nil {
			t.Fatal(err)
		}
	}
	before, err := tx.ExtensionResources().GetView(ctx, initial.Resource.FullResourceName())
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, store.DB, os.DirFS("migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 4); err != nil {
		t.Fatal(err)
	}
	var previous domain.ExtensionResourceRevision
	if err := store.DB.QueryRowContext(ctx, `SELECT update_revision FROM extension_resource_managed WHERE extension_resource_uid = $1`, initial.Resource.UID().String()).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	if previous != before.Resource.Revision() {
		t.Fatal("downgrade lost the managed revision before testing the upgrade")
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	read, err := store.BeginReadOnly(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.RollbackOnExit()
	after, err := read.ExtensionResources().GetView(ctx, initial.Resource.FullResourceName())
	if err != nil {
		t.Fatal(err)
	}
	if after.Resource.Revision() != previous || after.Etag() != before.Etag() || after.Intent.Version != before.Intent.Version {
		t.Fatal("moving the revision to the root lost a fence or changed managed intent")
	}
	restored, err := read.ExtensionResources().GetByUID(ctx, unmanaged.UID())
	if err != nil {
		t.Fatal(err)
	}
	if restored.Managed() != nil || restored.Revision() != 0 || restored.Labels()["owner"] != "user" {
		t.Fatal("migration failed to initialize an unmanaged ER's root revision")
	}
}
