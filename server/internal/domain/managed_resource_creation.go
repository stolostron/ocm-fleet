package domain

// ExtensionResourceCreateResult distinguishes a newly accepted Create from a
// persisted name-conflict rejection and a previously decided request. None of
// these outcomes is a storage error; callers must commit a new acceptance or
// rejection before returning it to the client.
type ExtensionResourceCreateResult uint8

const (
	// ExtensionResourceCreated means the resource and its acceptance were saved.
	ExtensionResourceCreated ExtensionResourceCreateResult = iota
	// ExtensionResourceCreateRejected means only a name-conflict decision was
	// saved. The resource occupying the name was not changed.
	ExtensionResourceCreateRejected
	// ExtensionResourceCreateAlreadyDecided means the nonce already has an
	// immutable decision. The attempted aggregate was not saved; GetCreation
	// resolves the original decision and its resource/caller scope.
	ExtensionResourceCreateAlreadyDecided
)

// ManagedResourceCreation is a read model for a compact creation decision owned
// by an [ExtensionResource]. Its request ID is also the creation nonce. A zero UID
// records AlreadyExists; an accepted creation identifies one incarnation, even
// after that resource has been deleted. No resource payload or credentials are
// retained in this decision. Resource and caller scope are explicit; retries do
// not compare payloads or update an already decided attempt.
type ManagedResourceCreation struct {
	requestID    string
	resourceType ResourceType
	name         ResourceName
	caller       FederatedIdentity
	hasCaller    bool
	uid          ExtensionResourceUID
}

func newManagedResourceCreation(requestID string, rt ResourceType, name ResourceName, caller *FederatedIdentity, uid ExtensionResourceUID) ManagedResourceCreation {
	c := ManagedResourceCreation{requestID: requestID, resourceType: rt, name: name, uid: uid}
	if caller != nil {
		c.caller = *caller
		c.hasCaller = true
	}
	return c
}

// RequestID returns the client-supplied creation nonce.
func (c ManagedResourceCreation) RequestID() string { return c.requestID }

// ResourceType returns the resource type to which the nonce is bound.
func (c ManagedResourceCreation) ResourceType() ResourceType { return c.resourceType }

// Name returns the resource name to which the nonce is bound.
func (c ManagedResourceCreation) Name() ResourceName { return c.name }

// Caller returns a copy of the original caller's identity, or nil for an
// anonymous attempt. Credentials and mutable subject claims are not retained.
func (c ManagedResourceCreation) Caller() *FederatedIdentity {
	if !c.hasCaller {
		return nil
	}
	caller := c.caller
	return &caller
}

// MatchesScope reports whether a retry addresses the same resource and caller.
// Payload differences do not reopen or replace a committed creation decision.
func (c ManagedResourceCreation) MatchesScope(rt ResourceType, name ResourceName, caller *FederatedIdentity) bool {
	if c.resourceType != rt || c.name != name {
		return false
	}
	if caller == nil {
		return !c.hasCaller
	}
	return c.hasCaller && c.caller == *caller
}

// UID identifies the accepted incarnation, or is zero for a rejected attempt.
func (c ManagedResourceCreation) UID() ExtensionResourceUID { return c.uid }

// AlreadyExists reports a retained name-conflict rejection.
func (c ManagedResourceCreation) AlreadyExists() bool { return c.uid.IsZero() }

// ManagedResourceWork identifies current work needing dispatch, rather than a
// historical mutation. Generation gaps and unfinished deletion are durable
// recovery state even when a conditional activity replay fails its old etag.
type ManagedResourceWork struct {
	UID           ExtensionResourceUID
	ResourceType  ResourceType
	Name          ResourceName
	FulfillmentID FulfillmentID
	Deleting      bool
	Paused        bool
	CleanupReady  bool
}
