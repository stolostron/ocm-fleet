package fleet_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stolostron/ocm-fleet/controllers/record/fleet"
)

func TestNotFound_RequiresResourceErrorCode(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		absent     bool
	}{
		{"resource absent", `{"code":5,"message":"resource not found"}`, true},
		{"addon route unavailable", "404 page not found", false},
		{"malformed error", `{"code":5,"message":42}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			remote, err := fleet.NewClient(server.URL, server.Client(), func(context.Context) (string, error) { return "test-token", nil })
			if err != nil {
				t.Fatal(err)
			}
			_, err = remote.Get(context.Background(), "records/example")
			if fleet.IsNotFound(err) != tc.absent {
				t.Fatalf("unsafe absence classification: %v", err)
			}
		})
	}
}

func TestAlreadyExists_RequiresConflictResponse(t *testing.T) {
	for _, status := range []int{http.StatusConflict, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"code":6,"message":"already exists"}`))
			}))
			defer server.Close()
			remote, err := fleet.NewClient(server.URL, server.Client(), func(context.Context) (string, error) { return "test-token", nil })
			if err != nil {
				t.Fatal(err)
			}
			_, err = remote.Create(context.Background(), "records/example", "request-id", fleet.Spec{})
			if fleet.IsAlreadyExists(err) != (status == http.StatusConflict) {
				t.Fatalf("transient gateway responses cannot resolve an ambiguous create: %v", err)
			}
		})
	}
}
