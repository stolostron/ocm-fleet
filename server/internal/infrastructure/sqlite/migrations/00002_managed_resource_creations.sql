-- +goose Up
-- Creation decisions belong to the extension-resource repository and outlive
-- hard deletion. Decisions do not expire; retention and garbage collection
-- are deferred for a separate design.
CREATE TABLE managed_resource_creations (
    request_id TEXT PRIMARY KEY CHECK (request_id <> ''),
    resource_type TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    caller_subject TEXT,
    caller_issuer TEXT,
    resource_uid TEXT,
    CHECK ((caller_subject IS NULL) = (caller_issuer IS NULL))
);

-- Recovery reads only current pending work, not mutation history.
CREATE INDEX idx_managed_fulfillment ON extension_resource_managed(fulfillment_id);
CREATE INDEX idx_fulfillments_pending ON fulfillments(id)
    WHERE state = 'deleting' OR
        (generation > observed_generation AND pause_reason = '');

-- +goose Down
DROP INDEX idx_fulfillments_pending;
DROP INDEX idx_managed_fulfillment;
DROP TABLE managed_resource_creations;
