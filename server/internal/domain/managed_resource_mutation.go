package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

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

func mutationActor(auth DeliveryAuth) *FederatedIdentity {
	if auth.Caller == nil {
		return nil
	}
	return &auth.Caller.FederatedIdentity
}
