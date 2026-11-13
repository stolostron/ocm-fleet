package domain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ManagedResourceMutationReceipt is an immutable result stored in the same
// transaction as a managed-resource mutation. It survives resource deletion:
// retrying an old Create must never create a second incarnation. Receipts also
// close the activity-commit/checkpoint gap in durable workflows.
type ManagedResourceMutationReceipt struct {
	RequestID     string
	Fingerprint   string
	View          ExtensionResourceView
	AlreadyExists bool
}

// ManagedResourceMutationRepository stores request receipts independently of
// the resource lifecycle. A request ID may identify only one logical input.
type ManagedResourceMutationRepository interface {
	Get(ctx context.Context, requestID string) (ManagedResourceMutationReceipt, error)
	Create(ctx context.Context, receipt ManagedResourceMutationReceipt) error
}

// mutationRequestID supplies a deterministic fallback for direct workflow
// invocation. Application services normally supply a UUID before scheduling,
// so distinct executions, including same-name recreations, have distinct IDs.
func mutationRequestID(record Record, supplied string) string {
	if supplied != "" {
		return supplied
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(record.ID())).String()
}

// mutationFingerprint compares request meaning, independently of JSON object
// key ordering. Authentication tokens are deliberately excluded by callers so
// a client can refresh credentials while retrying the same request.
func mutationFingerprint(input any) (string, error) {
	b, err := canonicalJSON(input)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

// canonicalJSON orders object keys and removes insignificant whitespace while
// preserving numeric precision. Both request identity and concurrency tokens
// must survive JSON storage normalization.
func canonicalJSON(input any) ([]byte, error) {
	b, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func loadMutationReceipt(ctx context.Context, tx Tx, id, fingerprint string) (*ManagedResourceMutationReceipt, error) {
	receipt, err := tx.ManagedResourceMutations().Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if receipt.Fingerprint != fingerprint {
		return nil, TerminalError(fmt.Errorf("%w: request_id was already used for different input", ErrInvalidArgument))
	}
	return &receipt, nil
}

func (r ManagedResourceMutationReceipt) result() (ExtensionResourceView, error) {
	if r.AlreadyExists {
		return ExtensionResourceView{}, TerminalError(fmt.Errorf("%w: managed resource already exists", ErrAlreadyExists))
	}
	return r.View, nil
}

func mutationActor(auth DeliveryAuth) *FederatedIdentity {
	if auth.Caller == nil {
		return nil
	}
	return &auth.Caller.FederatedIdentity
}
