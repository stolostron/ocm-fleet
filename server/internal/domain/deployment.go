package domain

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DeploymentUID is the opaque, stable identifier for a deployment
// instance. Generated once at creation time and never changes. The
// underlying type is [uuid.UUID] so structural validity is encoded
// in the type system. The nil UUID is invalid; parsing and hydration reject it.
//
//domain:constructors NewDeploymentUID ParseDeploymentUID
//domain:serialization json text sql
type DeploymentUID uuid.UUID

// NewDeploymentUID generates a new random [DeploymentUID].
func NewDeploymentUID() DeploymentUID {
	return DeploymentUID(uuid.New())
}

// ParseDeploymentUID parses a string into a non-nil [DeploymentUID].
func ParseDeploymentUID(s string) (DeploymentUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return DeploymentUID{}, fmt.Errorf("deployment uid: %w", err)
	}
	if u == uuid.Nil {
		return DeploymentUID{}, fmt.Errorf("deployment uid: %w: must not be nil", ErrInvalidArgument)
	}
	return DeploymentUID(u), nil
}

// String returns the canonical UUID string representation.
func (u DeploymentUID) String() string { return uuid.UUID(u).String() }

// MarshalText implements [encoding.TextMarshaler] for JSON string encoding.
func (u DeploymentUID) MarshalText() ([]byte, error) { return uuid.UUID(u).MarshalText() }

// UnmarshalText implements [encoding.TextUnmarshaler] with checked restoration.
func (u *DeploymentUID) UnmarshalText(data []byte) error {
	value, err := ParseDeploymentUID(string(data))
	if err != nil {
		return err
	}
	*u = value
	return nil
}

// UnmarshalJSON restores a required UID. A JSON null is invalid; optional UIDs
// use pointers so the enclosing representation can express absence.
func (u *DeploymentUID) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	return u.UnmarshalText([]byte(raw))
}

// Value implements [driver.Valuer] for SQL persistence.
func (u DeploymentUID) Value() (driver.Value, error) { return uuid.UUID(u).String(), nil }

// Scan implements [sql.Scanner] for SQL hydration.
func (u *DeploymentUID) Scan(src any) error {
	var raw uuid.UUID
	if err := raw.Scan(src); err != nil {
		return fmt.Errorf("deployment uid: %w", err)
	}
	value, err := ParseDeploymentUID(raw.String())
	if err != nil {
		return err
	}
	*u = value
	return nil
}

// IsZero returns true when the UID is the zero (nil) UUID.
func (u DeploymentUID) IsZero() bool { return uuid.UUID(u) == uuid.Nil }

// Deployment is the thin user-facing aggregate. It holds
// deployment-specific identity and a reference to the [Fulfillment]
// that owns the orchestration state.
//
// Construct new instances with [NewDeployment]; reconstitute from
// persistence with [DeploymentFromSnapshot]. Read via accessor methods.
type Deployment struct {
	name          ResourceName
	uid           DeploymentUID
	fulfillmentID FulfillmentID
	createdAt     time.Time
	updatedAt     time.Time
}

// NewDeployment creates a brand-new [Deployment]. Use this on creation
// paths; use [DeploymentFromSnapshot] only for reconstituting from
// persistence.
func NewDeployment(name ResourceName, uid DeploymentUID, fulfillmentID FulfillmentID, now time.Time) Deployment {
	return Deployment{
		name:          name,
		uid:           uid,
		fulfillmentID: fulfillmentID,
		createdAt:     now,
		updatedAt:     now,
	}
}

// Name returns the collection-qualified resource name
// (e.g. "deployments/my-deploy").
func (d Deployment) Name() ResourceName { return d.name }

// UID returns the deployment's external UID.
func (d Deployment) UID() DeploymentUID { return d.uid }

// FulfillmentID returns the linked fulfillment's identifier.
func (d Deployment) FulfillmentID() FulfillmentID { return d.fulfillmentID }

// CreatedAt returns the creation timestamp.
func (d Deployment) CreatedAt() time.Time { return d.createdAt }

// UpdatedAt returns the last-updated timestamp.
func (d Deployment) UpdatedAt() time.Time { return d.updatedAt }

// DeploymentView is the read model that joins a [Deployment] with its
// [Fulfillment]. Constructed by the repository via joins; never
// written directly. The transport layer maps this to the proto
// Deployment message.
type DeploymentView struct {
	Deployment  Deployment
	Fulfillment Fulfillment
}
