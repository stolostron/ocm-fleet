package extensionresource_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stolostron/ocm-fleet/server/internal/transport/extensionresource"
)

func TestDynamicManagedUpdate_HTTPAndConditionalDelete(t *testing.T) {
	env := setup(t)
	prefix := env.svc.Config.CanonicalHTTPPrefix()
	handler := extensionresource.BuildHTTPHandler(env.svc, env.conn, prefix)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(method, prefix+path, strings.NewReader(body)))
		return rec
	}
	created := request(http.MethodPost, "?cluster_id=update-http&request_id=3a5b14e0-fd56-4b9a-87cf-e381f7d67dfa", `{"spec":{"name":"update-http"}}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	updated := request(http.MethodPatch, "/update-http?update_mask=spec&request_id=edcdbbe3-1f9b-4694-9264-91946152338a", `{"spec":{"name":"update-http","nodes":[{"role":"control-plane"},{"role":"worker"}]}}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update: %d %s", updated.Code, updated.Body.String())
	}
	var view struct {
		UID           string `json:"uid"`
		Generation    string `json:"generation"`
		IntentVersion string `json:"intentVersion"`
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.UID == "" || view.Generation != "2" || view.IntentVersion != "2" {
		t.Fatalf("updated identity/generation: %+v", view)
	}
	stale := request(http.MethodPatch, "/update-http?update_mask=spec", `{"etag":"stale","spec":{"name":"update-http"}}`)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale update: %d %s", stale.Code, stale.Body.String())
	}
	invalid := request(http.MethodPatch, "/update-http?update_mask=state", `{"spec":{"name":"update-http"}}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("output-only mask: %d %s", invalid.Code, invalid.Body.String())
	}
	staleDelete := request(http.MethodDelete, "/update-http?etag=stale", "")
	if staleDelete.Code != http.StatusConflict {
		t.Fatalf("stale delete: %d %s", staleDelete.Code, staleDelete.Body.String())
	}
}
