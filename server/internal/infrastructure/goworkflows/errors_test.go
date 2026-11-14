package goworkflows

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/cschleiden/go-workflows/workflow"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func TestDomainErrorSurvivesWorkflowSerialization(t *testing.T) {
	for _, kind := range []error{domain.ErrStaleGeneration, domain.ErrAlreadyExists, domain.ErrNotFound, domain.ErrCreationRetired, domain.ErrInvalidArgument} {
		t.Run(kind.Error(), func(t *testing.T) {
			encoded := encodeDomainError(domain.TerminalError(fmt.Errorf("mutation: %w", kind)))
			data, err := json.Marshal(workflow.NewError(encoded))
			if err != nil {
				t.Fatal(err)
			}
			var restored workflow.Error
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			decoded := restoreDomainError(&restored)
			if !errors.Is(decoded, kind) || workflow.CanRetry(&restored) {
				t.Fatalf("lost classification: %v", decoded)
			}
		})
	}
	// User-controlled text and ordinary errors must never be classified by
	// substring. Only the adapter's explicit serialized error type is trusted.
	ordinary := errors.New("terminal: stale generation: not a domain failure")
	if errors.Is(restoreDomainError(workflow.NewError(ordinary)), domain.ErrStaleGeneration) {
		t.Fatal("classified ordinary text as a domain error")
	}
}
