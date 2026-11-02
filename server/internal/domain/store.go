package domain

import "context"

// Store provides transactional access to all repositories. Use
// [Store.Begin] to start a read-write transaction or
// [Store.BeginReadOnly] for a read-only transaction, then access
// repositories through the returned [Tx].
type Store interface {
	// Begin starts a read-write transaction. On SQLite this issues
	// BEGIN IMMEDIATE so writers are serialized and read-lock-upgrade
	// deadlocks cannot occur.
	Begin(ctx context.Context) (Tx, error)

	// BeginReadOnly starts a read-only transaction. It uses a
	// deferred lock so it never contends with other readers or
	// writers. Use this for queries that do not mutate state.
	BeginReadOnly(ctx context.Context) (Tx, error)
}

// Tx is a transaction that provides access to all repositories.
// All repository operations performed through a single Tx share the
// same underlying transaction. Call [Tx.Commit] to persist changes
// or [Tx.Rollback] to discard them. Use [Tx.RollbackOnExit] to attempt
// cleanup and report unexpected failures, including when the operation panics:
//
//	tx, err := store.Begin(ctx)
//	if err != nil { return err }
//	defer tx.RollbackOnExit()
//	// ... use tx ...
//	return tx.Commit()
type Tx interface {
	Targets() TargetRepository
	Fulfillments() FulfillmentRepository
	Deployments() DeploymentRepository
	Deliveries() DeliveryRepository
	Inventory() InventoryRepository
	ExtensionResources() ExtensionResourceRepository
	SignerEnrollments() SignerEnrollmentRepository
	ResourceIdentities() ResourceIdentityRepository
	// Queries provides the read model query surface over extension
	// resources. See [QueryRepository]'s doc for why this is not
	// grouped with the aggregate repositories above, and for the
	// deferred status of platform aggregate search.
	Queries() QueryRepository
	// Commit returns an error if the transaction has already ended.
	Commit() error
	// Rollback returns an error if rollback fails. It is a no-op after a
	// previous explicit rollback or commit attempt.
	Rollback() error
	// RollbackOnExit rolls back if needed and observes unexpected errors.
	RollbackOnExit()
}

// StoreObserver observes failures during transaction cleanup.
type StoreObserver interface {
	RollbackFailed(ctx context.Context, err error)
}

// NoOpStoreObserver ignores transaction cleanup failures.
type NoOpStoreObserver struct{}

func (NoOpStoreObserver) RollbackFailed(context.Context, error) {}
