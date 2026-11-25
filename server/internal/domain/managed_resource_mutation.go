package domain

import (
	"bytes"
	"encoding/json"

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

// canonicalJSON orders object keys and removes insignificant whitespace while
// preserving numeric precision. Spec comparisons and concurrency tokens must
// survive JSON storage normalization.
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
