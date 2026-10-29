// Package sqltxtest provides checked cleanup for database/sql transactions.
package sqltxtest

import (
	"database/sql"
	"errors"
	"testing"
)

// Rollback reports an unexpected transaction cleanup failure to the test.
func Rollback(testingT testing.TB, tx *sql.Tx) {
	testingT.Helper()
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		testingT.Errorf("rollback: %v", err)
	}
}
