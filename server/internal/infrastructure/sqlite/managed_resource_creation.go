package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

// GetCreation serializes one logical creation attempt and all creators of its
// name. These locks last until the surrounding write transaction finishes.
func (r *ExtensionResourceRepo) GetCreation(ctx context.Context, requestID string, name domain.FullResourceName) (domain.ManagedResourceCreation, error) {
	// Store.Begin uses BEGIN IMMEDIATE, which serializes SQLite creators,
	// including those whose request or resource name has no row yet.

	var s domain.ManagedResourceCreationSnapshot
	var uid sql.NullString
	var retainUntil sql.NullInt64
	err := r.DB.QueryRowContext(ctx,
		`SELECT request_id, fingerprint, resource_uid, retain_until
   FROM managed_resource_creations WHERE request_id = ?`, requestID).
		Scan(&s.RequestID, &s.Fingerprint, &uid, &retainUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ManagedResourceCreation{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.ManagedResourceCreation{}, err
	}
	if uid.Valid {
		s.UID, err = domain.ParseExtensionResourceUID(uid.String)
		if err != nil {
			return domain.ManagedResourceCreation{}, fmt.Errorf("decode creation UID: %w", err)
		}
	}
	if retainUntil.Valid {
		s.RetainUntil = time.Unix(0, retainUntil.Int64).UTC()
	}
	return domain.ManagedResourceCreationFromSnapshot(s), nil
}

// PutCreation is used only after GetCreation has locked the request and name.
// An expired decision can then be replaced without an unlocked check/write gap.
func (r *ExtensionResourceRepo) PutCreation(ctx context.Context, creation domain.ManagedResourceCreation) error {
	s := creation.Snapshot()
	var uid, retainUntil any
	if !s.UID.IsZero() {
		uid = s.UID.String()
	}
	if !s.RetainUntil.IsZero() {
		retainUntil = s.RetainUntil.UnixNano()
	}
	_, err := r.DB.ExecContext(ctx,
		`INSERT INTO managed_resource_creations (request_id, fingerprint, resource_uid, retain_until)
   VALUES (?, ?, ?, ?)
   ON CONFLICT (request_id) DO UPDATE SET fingerprint = excluded.fingerprint,
    resource_uid = excluded.resource_uid, retain_until = excluded.retain_until`,
		s.RequestID, s.Fingerprint, uid, retainUntil)
	return err
}

func (r *ExtensionResourceRepo) RetireCreation(ctx context.Context, uid domain.ExtensionResourceUID, retainUntil time.Time) error {
	_, err := r.DB.ExecContext(ctx,
		`UPDATE managed_resource_creations SET retain_until = ?
   WHERE resource_uid = ? AND retain_until IS NULL`,
		retainUntil.UnixNano(), uid.String())
	return err
}

func (r *ExtensionResourceRepo) PurgeExpiredCreations(ctx context.Context, now time.Time, limit int) (int, error) {
	result, err := r.DB.ExecContext(ctx,
		`DELETE FROM managed_resource_creations WHERE request_id IN (
   SELECT request_id FROM managed_resource_creations WHERE retain_until <= ?
   ORDER BY retain_until, request_id LIMIT ?)`,
		now.UnixNano(), limit)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	return int(count), err
}

// ListManagedResourceWork recovers from persisted generation gaps and unfinished
// deletion, including failures between a mutation commit and workflow dispatch.
func (r *ExtensionResourceRepo) ListManagedResourceWork(ctx context.Context, after domain.ExtensionResourceUID, limit int) ([]domain.ManagedResourceWork, error) {
	cursor := after.String()
	// A terminal failure observes its generation. A later edit can create
	// a new gap while the previous failed lifecycle state remains visible.
	rows, err := r.DB.QueryContext(ctx,
		`SELECT er.uid, er.service_name, er.type_name, er.collection_name, er.resource_id,
   m.fulfillment_id, f.state, f.pause_reason, f.generation, f.observed_generation
   FROM extension_resources er
   JOIN extension_resource_managed m ON m.extension_resource_uid = er.uid
   JOIN fulfillments f ON f.id = m.fulfillment_id
   WHERE er.uid > ? AND (f.state = 'deleting' OR
    (f.generation > f.observed_generation AND f.pause_reason = ''))
   ORDER BY er.uid LIMIT ?`, cursor, limit)
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
