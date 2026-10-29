package sqlite

import (
	"context"
	"testing"
)

func TestStoreRollbackFailureIsObserved(testingT *testing.T) {
	ctx := context.Background()
	observer := &recordingStoreObserver{}
	store := &Store{DB: OpenTestDB(testingT), Observer: observer}
	tx, err := store.Begin(ctx)
	if err != nil {
		testingT.Fatalf("Begin: %v", err)
	}
	defer tx.RollbackOnExit()
	if _, err := tx.(*storeTx).tx.ExecContext(ctx, "ROLLBACK"); err != nil {
		testingT.Fatalf("force transaction to end: %v", err)
	}
	tx.RollbackOnExit()
	if len(observer.failures) != 1 {
		testingT.Fatalf("rollback failures = %v, want one", observer.failures)
	}

	readTx, err := store.BeginReadOnly(ctx)
	if err != nil {
		testingT.Fatalf("BeginReadOnly after rollback failure: %v", err)
	}
	defer readTx.RollbackOnExit()
	var result int
	if err := readTx.(*storeTx).tx.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
		testingT.Fatalf("query after rollback failure: %v", err)
	}
	if result != 1 {
		testingT.Errorf("query result = %d, want 1", result)
	}
}

type recordingStoreObserver struct {
	failures []error
}

func (observer *recordingStoreObserver) RollbackFailed(_ context.Context, err error) {
	observer.failures = append(observer.failures, err)
}
