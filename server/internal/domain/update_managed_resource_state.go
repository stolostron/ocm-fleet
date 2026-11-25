package domain

import (
	"errors"
	"strings"
)

// UpdateManagedResourceState is the durable input to an Update workflow run.
// Adapters initialize only Input. ContinueAsNew carries the pinned target and
// resolved result forward, so recovery never rebases the caller's mutation on
// a newer etag or resource incarnation.
type UpdateManagedResourceState struct {
	Input   UpdateManagedResourceInput
	Target  *ManagedResourceUpdateTarget
	Result  *ManagedResourceUpdateResult
	Settled bool
}

// ManagedResourceUpdateTarget pins the original incarnation and precondition
// before any mutation can run. Revision is the repository's compare-and-swap
// precondition; Etag also covers changes to the resource's other visible state.
type ManagedResourceUpdateTarget struct {
	ResourceType  ResourceType
	Name          ResourceName
	UID           ExtensionResourceUID
	FulfillmentID FulfillmentID
	Revision      ExtensionResourceRevision
	Etag          Etag
}

// PreparedManagedResourceUpdate gives every mutation attempt the same target
// and precondition, including attempts after activity or workflow retries.
type PreparedManagedResourceUpdate struct {
	Input  UpdateManagedResourceInput
	Target ManagedResourceUpdateTarget
}

// ManagedResourceUpdateResult preserves the API outcome while the workflow
// finishes its progress obligation. It lives in workflow history, not in a
// resource mutation ledger. FailureKind preserves sentinel identity across
// ContinueAsNew without attempting to serialize a Go error.
type ManagedResourceUpdateResult struct {
	View        ExtensionResourceView
	Failure     string
	FailureKind string
}

var managedResourceUpdateFailureKinds = []struct {
	code string
	err  error
}{
	{"stale_generation", ErrStaleGeneration},
	{"invalid_argument", ErrInvalidArgument},
	{"not_found", ErrNotFound},
	{"auth_expired", ErrAuthExpired},
	{"unimplemented", ErrUnimplemented},
}

func managedResourceUpdateResult(view ExtensionResourceView, err error) *ManagedResourceUpdateResult {
	result := &ManagedResourceUpdateResult{View: view}
	if err != nil {
		result.Failure = strings.TrimPrefix(err.Error(), terminalPrefix)
		for _, kind := range managedResourceUpdateFailureKinds {
			if errors.Is(err, kind.err) {
				result.FailureKind = kind.code
				break
			}
		}
	}
	return result
}

func (r *ManagedResourceUpdateResult) err() error {
	if r.Failure == "" {
		return nil
	}
	var kind error
	for _, candidate := range managedResourceUpdateFailureKinds {
		if r.FailureKind == candidate.code {
			kind = candidate.err
			break
		}
	}
	return TerminalError(&managedResourceUpdateFailure{message: r.Failure, kind: kind})
}

type managedResourceUpdateFailure struct {
	message string
	kind    error
}

func (e *managedResourceUpdateFailure) Error() string { return e.message }
func (e *managedResourceUpdateFailure) Unwrap() error { return e.kind }
