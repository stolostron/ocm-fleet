package testserver

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/bootstrap"
)

// StartRecords starts the production record addon and durable workflow engine
// against a temporary SQLite database. Only OIDC discovery and verification
// are replaced: requests may authenticate with any nonempty bearer token.
// The returned URL serves the real Fleet HTTP API, including dynamic routes.
func StartRecords(t *testing.T) string {
	t.Helper()
	cfg, err := bootstrap.NewConfig(bootstrap.ConfigInput{
		GRPCAddr: "127.0.0.1:0", HTTPAddr: "127.0.0.1:0",
		DBPath: filepath.Join(t.TempDir(), "fleet.db"), Addons: "record",
		OIDCIssuer: "https://test-issuer.example", OIDCResourceAudience: "resource-manager",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv, err := bootstrap.Start(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)),
		bootstrap.WithOIDCDeps(bootstrap.OIDCDeps{Discovery: stubDiscovery{}, Verifier: stubVerifier{}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return "http://" + srv.Endpoints().HTTP.Dial
}
