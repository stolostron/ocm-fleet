package goworkflows

import (
	"errors"

	"github.com/cschleiden/go-workflows/workflow"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

// domainErrorKinds is an explicit wire vocabulary for domain failures. The
// engine persists error types and causes, but cannot preserve Go sentinel
// identity. Never infer a code from arbitrary error message substrings.
var domainErrorKinds = []struct {
	code string
	kind error
}{
	{"fleet.stale_generation", domain.ErrStaleGeneration},
	{"fleet.already_exists", domain.ErrAlreadyExists},
	{"fleet.not_found", domain.ErrNotFound},
	{"fleet.creation_retired", domain.ErrCreationRetired},
	{"fleet.invalid_argument", domain.ErrInvalidArgument},
	{"fleet.unimplemented", domain.ErrUnimplemented},
	{"fleet.auth_expired", domain.ErrAuthExpired},
}

func encodeDomainError(err error) error {
	if err == nil {
		return nil
	}
	for _, entry := range domainErrorKinds {
		if errors.Is(err, entry.kind) {
			failure := *workflow.NewError(err).(*workflow.Error)
			failure.Type = entry.code
			failure.Permanent = domain.IsTerminal(err) || domain.IsAuthExpired(err)
			return &failure
		}
	}
	if domain.IsTerminal(err) || domain.IsAuthExpired(err) {
		return workflow.NewPermanentError(err)
	}
	return err
}

func restoreDomainError(err error) error {
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		failure, ok := cause.(*workflow.Error)
		if !ok {
			continue
		}
		for _, entry := range domainErrorKinds {
			if failure.Type == entry.code {
				return &restoredDomainError{err: err, kind: entry.kind}
			}
		}
	}
	return err
}

// Keep the serialized cause chain so another workflow boundary can encode it
// again. Is restores domain identity for services without exposing engine types.
type restoredDomainError struct{ err, kind error }

func (e *restoredDomainError) Error() string        { return e.err.Error() }
func (e *restoredDomainError) Unwrap() error        { return e.err }
func (e *restoredDomainError) Is(target error) bool { return errors.Is(e.kind, target) }
