-- +goose Up
-- Creation decisions belong to the extension-resource repository and outlive
-- hard deletion. NULL retain_until means an accepted, live incarnation.
-- Failed and retired attempts expire after the documented replay window.
CREATE TABLE managed_resource_creations (
    request_id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    resource_uid TEXT,
    retain_until BIGINT,
    CHECK (resource_uid IS NOT NULL OR retain_until IS NOT NULL)
);
CREATE INDEX idx_managed_resource_creation_expiry ON managed_resource_creations(retain_until, request_id)
    WHERE retain_until IS NOT NULL;
CREATE INDEX idx_managed_resource_creation_uid ON managed_resource_creations(resource_uid)
    WHERE resource_uid IS NOT NULL;

-- Recovery reads only current pending work, not mutation history.
CREATE INDEX idx_managed_fulfillment ON extension_resource_managed(fulfillment_id);
CREATE INDEX idx_fulfillments_pending ON fulfillments(id)
    WHERE state = 'deleting' OR
        (generation > observed_generation AND pause_reason = '');

-- +goose Down
DROP INDEX idx_fulfillments_pending;
DROP INDEX idx_managed_fulfillment;
DROP TABLE managed_resource_creations;
