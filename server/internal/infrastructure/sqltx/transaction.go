// Package sqltx implements the shared database/sql transaction lifecycle.
package sqltx

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

// Transaction manages completion and cleanup of a database/sql transaction.
type Transaction struct {
	tx       *sql.Tx
	ctx      context.Context
	observer domain.StoreObserver
	mu       sync.Mutex
	done     bool
}

// New wraps a transaction and uses a no-op observer when observer is nil.
func New(ctx context.Context, tx *sql.Tx, observer domain.StoreObserver) *Transaction {
	if observer == nil {
		observer = domain.NoOpStoreObserver{}
	}
	return &Transaction{tx: tx, ctx: ctx, observer: observer}
}

// Commit commits the transaction, or fails if it has already ended.
func (transaction *Transaction) Commit() error {
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.done {
		return sql.ErrTxDone
	}
	transaction.done = true
	return transaction.tx.Commit()
}

// Rollback rolls back the transaction unless a commit or rollback was attempted.
func (transaction *Transaction) Rollback() error {
	transaction.mu.Lock()
	defer transaction.mu.Unlock()
	if transaction.done {
		return nil
	}
	transaction.done = true
	return transaction.tx.Rollback()
}

// RollbackOnExit rolls back if needed and reports unexpected cleanup failures.
// A rollback initiated internally by database/sql after context cancellation
// may run asynchronously, and its error is not exposed to this method.
func (transaction *Transaction) RollbackOnExit() {
	err := transaction.Rollback()
	if err == nil || (errors.Is(err, sql.ErrTxDone) && transaction.ctx.Err() != nil) {
		return
	}
	transaction.observer.RollbackFailed(transaction.ctx, err)
}
