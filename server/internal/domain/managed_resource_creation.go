package domain

import "time"

// ManagedResourceCreationRetention is the replay protection window after a
// rejected Create or final deletion. Accepted, live creations do not expire.
// Requests arriving after this window may be treated as new attempts.
const ManagedResourceCreationRetention = 30 * 24 * time.Hour

// ManagedResourceCreation is a compact creation decision owned by the extension
// resource repository. Its request ID is also the creation nonce. A zero UID
// records AlreadyExists; an accepted creation identifies one incarnation, even
// after that resource has been deleted. No resource payload or credentials are
// retained in this decision.
type ManagedResourceCreation struct {
	requestID   string
	fingerprint string
	uid         ExtensionResourceUID
	retainUntil time.Time
}

// NewManagedResourceCreation records an accepted incarnation or, with a zero
// uid, a rejected attempt. Final deletion starts retention for accepted attempts.
func NewManagedResourceCreation(requestID, fingerprint string, uid ExtensionResourceUID, now time.Time) ManagedResourceCreation {
	c := ManagedResourceCreation{requestID: requestID, fingerprint: fingerprint, uid: uid}
	if uid.IsZero() {
		c.retainUntil = now.Add(ManagedResourceCreationRetention)
	}
	return c
}

// RequestID returns the client-supplied creation nonce.
func (c ManagedResourceCreation) RequestID() string { return c.requestID }

// Fingerprint binds the nonce to its original logical input and actor.
func (c ManagedResourceCreation) Fingerprint() string { return c.fingerprint }

// UID identifies the accepted incarnation, or is zero for a rejected attempt.
func (c ManagedResourceCreation) UID() ExtensionResourceUID { return c.uid }

// RetainUntil returns the replay deadline, or zero while acceptance is live.
func (c ManagedResourceCreation) RetainUntil() time.Time { return c.retainUntil }

// AlreadyExists reports a retained name-conflict rejection.
func (c ManagedResourceCreation) AlreadyExists() bool { return c.uid.IsZero() }

// Expired reports whether the documented replay window has elapsed. A live
// accepted incarnation has no expiry, regardless of how old its Create is.
func (c ManagedResourceCreation) Expired(now time.Time) bool {
	return !c.retainUntil.IsZero() && !now.Before(c.retainUntil)
}

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
