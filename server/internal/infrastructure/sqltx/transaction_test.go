package sqltx_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stolostron/ocm-fleet/server/internal/infrastructure/sqltx"
)

func TestRollbackOnExit(testingT *testing.T) {
	rollbackErr := errors.New("rollback failed")
	operationErr := errors.New("operation failed")
	for _, test := range []struct {
		name         string
		rollbackErr  error
		operationErr error
		wantFailure  bool
	}{
		{name: "successful rollback", operationErr: operationErr},
		{name: "rollback failure after operation failure", rollbackErr: rollbackErr, operationErr: operationErr, wantFailure: true},
		{name: "rollback failure after successful operation", rollbackErr: rollbackErr, wantFailure: true},
	} {
		testingT.Run(test.name, func(testingT *testing.T) {
			fake := newFakeTransaction()
			fake.rollbackErr = test.rollbackErr
			observer := &recordingObserver{}
			tx := sqltx.New(context.Background(), begin(testingT, context.Background(), fake), observer)

			err := func() error {
				defer tx.RollbackOnExit()
				return test.operationErr
			}()
			if !errors.Is(err, test.operationErr) {
				testingT.Fatalf("operation error = %v, want %v", err, test.operationErr)
			}
			tx.RollbackOnExit()
			if got := fake.rollbacks.Load(); got != 1 {
				testingT.Errorf("rollbacks = %d, want 1", got)
			}
			if test.wantFailure {
				if len(observer.failures) != 1 || !errors.Is(observer.failures[0], rollbackErr) {
					testingT.Fatalf("failures = %v, want rollback failure once", observer.failures)
				}
			} else if len(observer.failures) != 0 {
				testingT.Fatalf("unexpected failures: %v", observer.failures)
			}
		})
	}
}

func TestRollbackOnExitAfterCommit(testingT *testing.T) {
	fake := newFakeTransaction()
	observer := &recordingObserver{}
	tx := sqltx.New(context.Background(), begin(testingT, context.Background(), fake), observer)
	if err := tx.Commit(); err != nil {
		testingT.Fatalf("Commit: %v", err)
	}
	tx.RollbackOnExit()
	if got := fake.rollbacks.Load(); got != 0 {
		testingT.Errorf("rollbacks = %d, want 0", got)
	}
	if len(observer.failures) != 0 {
		testingT.Errorf("unexpected failures: %v", observer.failures)
	}
	if err := tx.Commit(); err == nil {
		testingT.Fatal("second Commit succeeded")
	}
}

func TestCommitAfterRollbackFails(testingT *testing.T) {
	fake := newFakeTransaction()
	tx := sqltx.New(context.Background(), begin(testingT, context.Background(), fake), &recordingObserver{})
	if err := tx.Rollback(); err != nil {
		testingT.Fatalf("Rollback: %v", err)
	}
	if err := tx.Commit(); err == nil {
		testingT.Fatal("Commit after Rollback succeeded")
	}
	if got := fake.commits.Load(); got != 0 {
		testingT.Errorf("commits = %d, want 0", got)
	}
}

func TestCommitAfterFailedCommitFails(testingT *testing.T) {
	fake := newFakeTransaction()
	fake.commitErr = errors.New("commit failed")
	tx := sqltx.New(context.Background(), begin(testingT, context.Background(), fake), &recordingObserver{})
	if err := tx.Commit(); !errors.Is(err, fake.commitErr) {
		testingT.Fatalf("Commit = %v, want %v", err, fake.commitErr)
	}
	if err := tx.Commit(); err == nil {
		testingT.Fatal("second Commit succeeded")
	}
	tx.RollbackOnExit()
	if got := fake.rollbacks.Load(); got != 0 {
		testingT.Errorf("rollbacks = %d, want 0", got)
	}
}

func TestRollbackOnExitAfterContextCancellation(testingT *testing.T) {
	for _, test := range []struct {
		name        string
		rollbackErr error
	}{
		{name: "successful automatic rollback"},
		{name: "automatic rollback error is not exposed", rollbackErr: errors.New("rollback failed")},
	} {
		testingT.Run(test.name, func(testingT *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			fake := newFakeTransaction()
			fake.rollbackErr = test.rollbackErr
			observer := &recordingObserver{}
			tx := sqltx.New(ctx, begin(testingT, ctx, fake), observer)
			cancel()
			<-fake.rolledBack
			tx.RollbackOnExit()
			if len(observer.failures) != 0 {
				testingT.Fatalf("automatic rollback reported as failure: %v", observer.failures)
			}
		})
	}
}

func TestRollbackOnExitReportsUnexpectedlyEndedTransaction(testingT *testing.T) {
	fake := newFakeTransaction()
	observer := &recordingObserver{}
	rawTx := begin(testingT, context.Background(), fake)
	tx := sqltx.New(context.Background(), rawTx, observer)
	if err := rawTx.Rollback(); err != nil {
		testingT.Fatalf("Rollback: %v", err)
	}
	tx.RollbackOnExit()
	if len(observer.failures) != 1 || !errors.Is(observer.failures[0], sql.ErrTxDone) {
		testingT.Fatalf("failures = %v, want unexpected ErrTxDone", observer.failures)
	}
}

func TestConcurrentRollbackOnExit(testingT *testing.T) {
	fake := newFakeTransaction()
	fake.rollbackErr = errors.New("rollback failed")
	observer := &recordingObserver{}
	tx := sqltx.New(context.Background(), begin(testingT, context.Background(), fake), observer)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			<-start
			tx.RollbackOnExit()
		})
	}
	close(start)
	workers.Wait()
	if got := fake.rollbacks.Load(); got != 1 {
		testingT.Errorf("rollbacks = %d, want 1", got)
	}
	if len(observer.failures) != 1 || !errors.Is(observer.failures[0], fake.rollbackErr) {
		testingT.Errorf("failures = %v, want rollback failure once", observer.failures)
	}
}

func TestRollbackOnExitDuringPanic(testingT *testing.T) {
	fake := newFakeTransaction()
	fake.rollbackErr = errors.New("rollback failed")
	observer := &recordingObserver{}
	tx := sqltx.New(context.Background(), begin(testingT, context.Background(), fake), observer)
	func() {
		defer func() {
			if got := recover(); got != "operation panic" {
				testingT.Errorf("panic = %v, want operation panic", got)
			}
		}()
		defer tx.RollbackOnExit()
		panic("operation panic")
	}()
	if got := fake.rollbacks.Load(); got != 1 {
		testingT.Errorf("rollbacks = %d, want 1", got)
	}
	if len(observer.failures) != 1 || !errors.Is(observer.failures[0], fake.rollbackErr) {
		testingT.Errorf("failures = %v, want rollback failure once", observer.failures)
	}
}

type recordingObserver struct {
	failures []error
}

func (observer *recordingObserver) RollbackFailed(_ context.Context, err error) {
	observer.failures = append(observer.failures, err)
}

func begin(testingT *testing.T, ctx context.Context, transaction *fakeTransaction) *sql.Tx {
	testingT.Helper()
	db := sql.OpenDB(fakeConnector{transaction: transaction})
	testingT.Cleanup(func() {
		if err := db.Close(); err != nil {
			testingT.Errorf("close database: %v", err)
		}
	})
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		testingT.Fatalf("BeginTx: %v", err)
	}
	return tx
}

type fakeConnector struct {
	transaction *fakeTransaction
}

func (connector fakeConnector) Connect(context.Context) (driver.Conn, error) {
	return fakeConnection(connector), nil
}

func (connector fakeConnector) Driver() driver.Driver { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, fmt.Errorf("use connector") }

type fakeConnection struct {
	transaction *fakeTransaction
}

func (connector fakeConnection) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepare unsupported")
}

func (connector fakeConnection) Close() error               { return nil }
func (connection fakeConnection) Begin() (driver.Tx, error) { return connection.transaction, nil }

type fakeTransaction struct {
	commitErr   error
	rollbackErr error
	commits     atomic.Int32
	rollbacks   atomic.Int32
	rolledBack  chan struct{}
}

func newFakeTransaction() *fakeTransaction {
	return &fakeTransaction{rolledBack: make(chan struct{})}
}

func (transaction *fakeTransaction) Commit() error {
	transaction.commits.Add(1)
	return transaction.commitErr
}

func (transaction *fakeTransaction) Rollback() error {
	transaction.rollbacks.Add(1)
	close(transaction.rolledBack)
	return transaction.rollbackErr
}
