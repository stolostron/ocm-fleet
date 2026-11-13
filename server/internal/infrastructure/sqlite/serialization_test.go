package sqlite_test

import (
	"testing"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
	"github.com/stolostron/ocm-fleet/server/internal/infrastructure/sqlite"
)

func TestDeploymentUIDSQLRoundTrip(t *testing.T) {
	db := sqlite.OpenTestDB(t)
	uid := domain.NewDeploymentUID()
	var restored domain.DeploymentUID
	if err := db.QueryRowContext(t.Context(), "SELECT ?", uid).Scan(&restored); err != nil {
		t.Fatal(err)
	}
	if restored != uid {
		t.Fatalf("restored %s, want %s", restored, uid)
	}

	for _, raw := range []any{nil, "", "invalid", "00000000-0000-0000-0000-000000000000"} {
		restored := uid
		if err := db.QueryRowContext(t.Context(), "SELECT ?", raw).Scan(&restored); err == nil {
			t.Fatalf("invalid persisted UID %v was accepted", raw)
		}
		if restored != uid { //nolint:domainzero // This assertion tests that failed hydration preserves the receiver.
			t.Fatal("failed SQL hydration changed the UID")
		}
	}
}
