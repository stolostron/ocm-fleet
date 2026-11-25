package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

// GetCreation reads a retained decision without locking. Create's unique inserts
// serialize concurrent attempts; this lookup is only needed for nonce replays.
func (r *ExtensionResourceRepo) GetCreation(ctx context.Context, requestID string) (domain.ManagedResourceCreation, error) {
	var s domain.ManagedResourceCreationSnapshot
	var uid, callerSubject, callerIssuer sql.NullString
	err := r.DB.QueryRowContext(ctx,
		`SELECT request_id, resource_type, resource_name, caller_subject, caller_issuer, resource_uid
   FROM managed_resource_creations WHERE request_id = $1`, requestID).
		Scan(&s.RequestID, &s.ResourceType, &s.Name, &callerSubject, &callerIssuer, &uid)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ManagedResourceCreation{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ManagedResourceCreation{}, err
	}
	if callerSubject.Valid {
		s.Caller = &domain.FederatedIdentity{Subject: domain.SubjectID(callerSubject.String), Issuer: domain.IssuerURL(callerIssuer.String)}
	}
	if uid.Valid {
		s.UID, err = domain.ParseExtensionResourceUID(uid.String)
		if err != nil {
			return domain.ManagedResourceCreation{}, fmt.Errorf("decode creation UID: %w", err)
		}
	}
	return domain.ManagedResourceCreationFromSnapshot(s), nil
}

// ListManagedResourceWork recovers from persisted generation gaps and unfinished
// deletion, including failures between a mutation commit and workflow dispatch.
func (r *ExtensionResourceRepo) ListManagedResourceWork(ctx context.Context, after domain.ExtensionResourceUID, limit int) ([]domain.ManagedResourceWork, error) {
	// The zero UUID sorts before every valid resource UID and is also a
	// valid PostgreSQL UUID parameter for the first page.
	cursor := after.String()
	// A terminal failure observes its generation. A later edit can create
	// a new gap while the previous failed lifecycle state remains visible.
	rows, err := r.DB.QueryContext(ctx,
		`SELECT er.uid, er.service_name, er.type_name, er.collection_name, er.resource_id,
   m.fulfillment_id, f.state, f.pause_reason, f.generation, f.observed_generation
   FROM extension_resources er
   JOIN extension_resource_managed m ON m.extension_resource_uid = er.uid
   JOIN fulfillments f ON f.id = m.fulfillment_id
   WHERE er.uid > $1 AND (f.state = 'deleting' OR
    (f.generation > f.observed_generation AND f.pause_reason = ''))
   ORDER BY er.uid LIMIT $2`, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var work []domain.ManagedResourceWork
	for rows.Next() {
		var item domain.ManagedResourceWork
		var service, typeName, collection, id, state, pause string
		var generation, observed domain.Generation
		if err := rows.Scan(&item.UID, &service, &typeName, &collection, &id, &item.FulfillmentID, &state, &pause, &generation, &observed); err != nil {
			return nil, err
		}
		item.ResourceType = domain.ResourceType(service + "/" + typeName)
		item.Name = domain.ResourceName(collection + "/" + id)
		item.Deleting = state == string(domain.FulfillmentStateDeleting)
		item.Paused = pause != ""
		item.CleanupReady = item.Deleting && !item.Paused && observed >= generation
		work = append(work, item)
	}
	return work, rows.Err()
}

// insertCreationDecision is part of Create's aggregate save. The
// projection deliberately has no cascading foreign key: decisions must outlive
// the attempt's resource. The first committed decision wins, even if another
// copy observed a different name-conflict outcome before reaching this insert.
func (r *ExtensionResourceRepo) insertCreationDecision(ctx context.Context, s domain.ManagedResourceCreationSnapshot) (bool, error) {
	var uid any
	var callerSubject, callerIssuer any
	if s.Caller != nil {
		callerSubject, callerIssuer = string(s.Caller.Subject), string(s.Caller.Issuer)
	}
	if !s.UID.IsZero() {
		uid = s.UID.String()
	}
	res, err := r.DB.ExecContext(ctx,
		`INSERT INTO managed_resource_creations (request_id, resource_type, resource_name, caller_subject, caller_issuer, resource_uid)
   VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (request_id) DO NOTHING`,
		s.RequestID, string(s.ResourceType), string(s.Name), callerSubject, callerIssuer, uid)
	if err != nil {
		return false, fmt.Errorf("persist creation decision: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
