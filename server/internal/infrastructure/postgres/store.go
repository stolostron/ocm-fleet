package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
	"github.com/stolostron/ocm-fleet/server/internal/infrastructure/sqltx"
)

var _ domain.Store = (*Store)(nil)

// Store implements [domain.Store] backed by Postgres.
type Store struct {
	DB *sql.DB

	// SchemaProvider is threaded into every QueryRepo this store
	// hands out (see storeTx.Queries), so query-time
	// resource.spec.*/resource.observation.* field validation and
	// activation scoping can use the activated type set (see
	// [domain.QuerySchemaProvider] and
	// [domain.ResolveQueryResourceTypeScope]). Nil is a valid,
	// permissive default (no activation IN constraint).
	SchemaProvider domain.QuerySchemaProvider

	// Observer receives transaction cleanup failures. Nil disables observation.
	Observer domain.StoreObserver
}

func (s *Store) Begin(ctx context.Context) (domain.Tx, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	return &storeTx{Transaction: sqltx.New(ctx, tx, s.Observer), tx: tx, schemaProvider: s.SchemaProvider}, nil
}

func (s *Store) BeginReadOnly(ctx context.Context) (domain.Tx, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin read-only tx: %w", err)
	}
	return &storeTx{Transaction: sqltx.New(ctx, tx, s.Observer), tx: tx, schemaProvider: s.SchemaProvider}, nil
}

type storeTx struct {
	*sqltx.Transaction
	tx             *sql.Tx
	schemaProvider domain.QuerySchemaProvider
}

func (t *storeTx) Targets() domain.TargetRepository { return &TargetRepo{DB: t.tx} }
func (t *storeTx) Fulfillments() domain.FulfillmentRepository {
	return &FulfillmentRepo{DB: t.tx}
}
func (t *storeTx) Deployments() domain.DeploymentRepository { return &DeploymentRepo{DB: t.tx} }
func (t *storeTx) Deliveries() domain.DeliveryRepository    { return &DeliveryRepo{DB: t.tx} }
func (t *storeTx) Inventory() domain.InventoryRepository    { return &InventoryRepo{DB: t.tx} }
func (t *storeTx) ExtensionResources() domain.ExtensionResourceRepository {
	return &ExtensionResourceRepo{DB: t.tx}
}
func (t *storeTx) SignerEnrollments() domain.SignerEnrollmentRepository {
	return &SignerEnrollmentRepo{DB: t.tx}
}
func (t *storeTx) ResourceIdentities() domain.ResourceIdentityRepository {
	return &ResourceIdentityRepo{DB: t.tx}
}
func (t *storeTx) Queries() domain.QueryRepository {
	return &QueryRepo{DB: t.tx, SchemaProvider: t.schemaProvider}
}
