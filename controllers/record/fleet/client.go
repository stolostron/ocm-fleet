// Package fleet implements the small Fleet HTTP surface needed by the record
// controller. The Kubernetes controller owns intent; Fleet owns execution status.
package fleet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const collectionPath = "/apis/records.open-cluster-management.io/v1alpha/records"

// Spec is the complete Fleet record configuration.
type Spec struct {
	Values map[string]string `json:"values,omitempty"`
}

// Record is the managed API view. Fleet uses protobuf JSON, including quoted
// int64 generations. Missing or malformed identity never counts as success.
type Record struct {
	Name               string `json:"name"`
	UID                string `json:"uid"`
	Etag               string `json:"etag"`
	Spec               Spec   `json:"spec"`
	Generation         int64  `json:"generation,string"`
	ObservedGeneration int64  `json:"observedGeneration,string"`
	State              string `json:"state"`
	Reconciling        bool   `json:"reconciling"`
	PauseReason        string `json:"pauseReason"`
	StatusReason       string `json:"statusReason"`
}

// Ready means the provider accepted the current intent. It is not a claim
// about external infrastructure: configuration records have no external effect.
func (r Record) Ready() bool {
	return r.State == "ACTIVE" && r.PauseReason == "" && !r.Reconciling && r.Generation > 0 && r.ObservedGeneration == r.Generation
}

// APIError preserves structured HTTP and gRPC codes without retaining tokens
// or request bodies. A conflict requires a fresh read before another mutation.
type APIError struct {
	StatusCode int    `json:"-"`
	Code       int    `json:"code"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Fleet HTTP %d (code %d): %s", e.StatusCode, e.Code, e.Message)
}

// IsNotFound reports a missing resource, not a missing HTTP route. An addon
// route can be unavailable while its persisted resources still exist.
func IsNotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.StatusCode == http.StatusNotFound && e.Code == 5
}

// IsConflict reports a failed concurrency precondition.
func IsConflict(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.StatusCode == http.StatusConflict
}

// IsAlreadyExists distinguishes Create collisions from etag conflicts.
func IsAlreadyExists(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.StatusCode == http.StatusConflict && e.Code == 6
}

// TokenSource loads credentials for each request, allowing projected token
// files to rotate without restarting the controller.
type TokenSource func(context.Context) (string, error)

// Client speaks to one configured Fleet endpoint. Use a bounded HTTP client
// with verified TLS in production; request contexts also bound every operation.
type Client struct {
	endpoint url.URL
	http     *http.Client
	token    TokenSource
}

// NewClient validates the endpoint and prevents credential-bearing redirects.
// The supplied HTTP client is copied; its transport and connection pool are shared.
func NewClient(endpoint string, httpClient *http.Client, token TokenSource) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("fleet endpoint: %w", err)
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("fleet endpoint must be an HTTP(S) origin or path prefix without credentials, query, or fragment")
	}
	if httpClient == nil || token == nil {
		return nil, fmt.Errorf("fleet HTTP client and token source are required")
	}
	c := *httpClient
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{endpoint: *u, http: &c, token: token}, nil
}

// Get reads fresh intent and execution status.
func (c *Client) Get(ctx context.Context, name string) (Record, error) {
	return c.request(ctx, http.MethodGet, name, nil, nil)
}

// Create seals a logical create request. Retry the same ID and spec until a
// definitive success or AlreadyExists; do not infer completion from a GET 404.
func (c *Client) Create(ctx context.Context, name, requestID string, spec Spec) (Record, error) {
	if _, err := recordID(name); err != nil {
		return Record{}, err
	}
	query := url.Values{"record_id": {strings.TrimPrefix(name, "records/")}, "request_id": {requestID}}
	return c.request(ctx, http.MethodPost, "", query, struct {
		Spec Spec `json:"spec"`
	}{spec})
}

// Update conditionally replaces the entire spec, preserving unowned metadata.
func (c *Client) Update(ctx context.Context, current Record, requestID string, spec Spec) (Record, error) {
	if current.Etag == "" {
		return Record{}, fmt.Errorf("fleet update requires an etag")
	}
	return c.request(ctx, http.MethodPatch, current.Name, url.Values{"update_mask": {"spec"}, "request_id": {requestID}}, struct {
		Name string `json:"name"`
		Etag string `json:"etag"`
		Spec Spec   `json:"spec"`
	}{current.Name, current.Etag, spec})
}

// Delete begins conditional deletion. A later GET 404 confirms completion.
func (c *Client) Delete(ctx context.Context, current Record, requestID string) error {
	if current.Etag == "" {
		return fmt.Errorf("fleet delete requires an etag")
	}
	_, err := c.request(ctx, http.MethodDelete, current.Name, url.Values{"etag": {current.Etag}, "request_id": {requestID}}, nil)
	return err
}

func (c *Client) request(ctx context.Context, method, name string, query url.Values, body any) (Record, error) {
	u := c.endpoint
	u.Path = strings.TrimRight(u.Path, "/") + collectionPath
	if name != "" {
		id, err := recordID(name)
		if err != nil {
			return Record{}, err
		}
		u.Path += "/" + id
	}
	u.RawQuery = query.Encode()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return Record{}, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(payload))
	if err != nil {
		return Record{}, err
	}
	token, err := c.token(ctx)
	if err != nil {
		return Record{}, fmt.Errorf("load Fleet credentials: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return Record{}, fmt.Errorf("fleet token is empty")
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Record{}, fmt.Errorf("fleet request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		return Record{}, fmt.Errorf("read Fleet response: %w", err)
	}
	if len(data) > 2<<20 {
		return Record{}, fmt.Errorf("fleet response exceeds 2 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := &APIError{StatusCode: resp.StatusCode}
		if err := json.Unmarshal(data, e); err != nil {
			e.Code = 0 // Do not trust fields from a partially decoded error.
			e.Message = http.StatusText(resp.StatusCode)
		} else if e.Message == "" {
			e.Message = http.StatusText(resp.StatusCode)
		}
		return Record{}, e
	}
	var result Record
	if err := json.Unmarshal(data, &result); err != nil {
		return Record{}, fmt.Errorf("decode Fleet response: %w", err)
	}
	if result.Name == "" || result.UID == "" || result.Etag == "" || result.Generation < 1 {
		return Record{}, fmt.Errorf("fleet response lacks managed resource identity or generation")
	}
	return result, nil
}

func recordID(name string) (string, error) {
	id, ok := strings.CutPrefix(name, "records/")
	if !ok || id == "" || strings.ContainsAny(id, "/:%?#\\") || id == "." || id == ".." {
		return "", fmt.Errorf("invalid Fleet record name %q", name)
	}
	return id, nil
}
