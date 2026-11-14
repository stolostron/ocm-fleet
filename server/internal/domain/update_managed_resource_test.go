package domain_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

func TestUpdateManagedResource_ConditionalIntentAndReplay(t *testing.T) {
	store, _ := setupStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Orchestration: &fakeOrchestrationWorkflow{}, Now: func() time.Time { return now }}
	initial, err := create.Run(&stubRecord{ctx: ctx}, domain.CreateManagedResourceInput{
		RequestID: "create-update-test", ResourceType: typeDef.ResourceType(), Name: "records/update", TypeDef: typeDef,
		Spec: json.RawMessage(`{"values":{"message":"before"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	update := &domain.UpdateManagedResourceWorkflowSpec{Store: store, Orchestration: &fakeOrchestrationWorkflow{}, Now: func() time.Time { return now.Add(time.Minute) }}
	in := domain.UpdateManagedResourceInput{
		RequestID: "update-test", ResourceType: typeDef.ResourceType(), Name: initial.Resource.Name(),
		Spec: json.RawMessage(`{"values":{"message":"after"}}`), UpdateSpec: true, Etag: initial.Etag(),
	}
	changed, err := update.Run(&stubRecord{ctx: ctx}, in)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Resource.UID() != initial.Resource.UID() || changed.Intent.Version != 2 || changed.Fulfillment.Generation() != 2 {
		t.Fatalf("update must preserve identity and advance intent/generation once: %+v", changed)
	}
	if !changed.Resource.UpdatedAt().Equal(now.Add(time.Minute)) {
		t.Fatal("spec update did not update the resource timestamp")
	}
	// A lost activity result may replay with its own now-stale etag. The
	// conditional mutation fails safely; generation recovery must dispatch
	// the already-committed intent without a historical response receipt.
	if _, err := update.Run(&stubRecord{ctx: ctx}, in); !errors.Is(err, domain.ErrStaleGeneration) {
		t.Fatalf("replay after commit must fail its original precondition: %v", err)
	}

	in.RequestID = "conflicting-update"
	in.Spec = json.RawMessage(`{"values":{"message":"stale"}}`)
	if _, err := update.Run(&stubRecord{ctx: ctx}, in); !errors.Is(err, domain.ErrStaleGeneration) {
		t.Fatalf("a competing writer with the old etag must fail: %v", err)
	}
	in.RequestID = "no-op-update"
	in.Etag = changed.Etag()
	in.Spec = json.RawMessage(`{ "values": { "message": "after" } }`)
	unchanged, err := update.Run(&stubRecord{ctx: ctx}, in)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Intent.Version != 2 || unchanged.Fulfillment.Generation() != 2 || unchanged.Etag() != changed.Etag() {
		t.Fatal("a semantically identical spec must not change intent, generation, or etag")
	}

}

func TestUpdateManagedResource_RejectsDeleting(t *testing.T) {
	store, _ := setupStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Orchestration: &fakeOrchestrationWorkflow{}}
	view, err := create.Run(&stubRecord{ctx: ctx}, domain.CreateManagedResourceInput{
		RequestID: "create-deleting", ResourceType: typeDef.ResourceType(), Name: "records/deleting", TypeDef: typeDef, Spec: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	f, err := tx.Fulfillments().Get(ctx, view.Fulfillment.ID())
	if err != nil {
		t.Fatal(err)
	}
	f.TransitionToDeleting(domain.DeliveryAuth{})
	if err := tx.Fulfillments().Update(ctx, f); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	update := &domain.UpdateManagedResourceWorkflowSpec{Store: store}
	_, err = update.MutateSpec().Run(ctx, domain.UpdateManagedResourceInput{
		RequestID: "update-deleting", ResourceType: typeDef.ResourceType(), Name: view.Resource.Name(), Spec: json.RawMessage(`{"changed":true}`), UpdateSpec: true,
	})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("update of deleting resource must fail: %v", err)
	}
}

func TestUpdateManagedResource_SignedIntentBindsNewSpecAndGeneration(t *testing.T) {
	store, _ := setupStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	typeDef := seedLifecycleResourceType(t, store, now)
	create := &domain.CreateManagedResourceWorkflowSpec{Store: store, Orchestration: &fakeOrchestrationWorkflow{}}
	initial, err := create.Run(&stubRecord{ctx: ctx}, domain.CreateManagedResourceInput{RequestID: "create-signed-update", ResourceType: typeDef.ResourceType(), Name: "records/signed", TypeDef: typeDef, Spec: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity := domain.FederatedIdentity{Subject: "record-controller", Issuer: "https://issuer.example.com"}
	tx, err := store.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.RollbackOnExit()
	if err := tx.SignerEnrollments().Create(ctx, domain.SignerEnrollmentFromSnapshot(domain.SignerEnrollmentSnapshot{ID: "record-signer", FederatedIdentity: identity, RegistrySubject: "record-user", RegistryID: "github.com"})); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	provenance := &domain.ProvenanceService{KeyResolver: &domain.KeyResolver{Registries: domain.BuiltInKeyRegistries(), Clients: map[domain.KeyRegistryType]domain.RegistryClient{domain.KeyRegistryTypeGitHub: &fakeRegistryClient{keys: map[string][]crypto.PublicKey{"https://api.github.com/record-user": {&key.PublicKey}}}}}}
	update := &domain.UpdateManagedResourceWorkflowSpec{Store: store, Orchestration: &fakeOrchestrationWorkflow{}, ProvenanceSvc: provenance}
	in := domain.UpdateManagedResourceInput{RequestID: "sign-updated-intent", ResourceType: typeDef.ResourceType(), Name: initial.Resource.Name(), Spec: json.RawMessage(`{"values":{"message":"signed"}}`), UpdateSpec: true, Etag: initial.Etag(), ExpectedGeneration: 2, ValidUntil: now.Add(time.Hour), Auth: domain.DeliveryAuth{Caller: &domain.SubjectClaims{FederatedIdentity: identity}}}
	doc, err := domain.BuildManagedResourceEnvelope(in.ResourceType, in.Name, in.Spec, in.ValidUntil, nil, in.ExpectedGeneration)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(doc)
	in.UserSignature, err = ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	changed, err := update.Run(&stubRecord{ctx: ctx}, in)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Fulfillment.Provenance() == nil || changed.Fulfillment.Provenance().ExpectedGeneration != 2 {
		t.Fatal("update did not bind provenance to the new generation")
	}
	ref := changed.Fulfillment.AttestationRef()
	if ref == nil || ref.RelationRef == nil || *ref.RelationRef != in.ResourceType {
		t.Fatal("signed upgrade lost the relation evidence needed for attestation")
	}
	in.RequestID = "unsigned-change-of-signed-record"
	in.Spec = json.RawMessage(`{"values":{"message":"unsigned"}}`)
	in.Etag = changed.Etag()
	in.ExpectedGeneration = 3
	in.UserSignature = nil
	if _, err := update.Run(&stubRecord{ctx: ctx}, in); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("changing signed intent without a fresh signature: %v", err)
	}
}
