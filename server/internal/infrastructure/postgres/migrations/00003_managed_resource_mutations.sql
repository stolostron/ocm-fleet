-- +goose Up
-- Receipts intentionally outlive the resource and its fulfillment. A delayed
-- retry of a completed Create must not resurrect a deleted incarnation.
CREATE TABLE managed_resource_mutations (
    request_id TEXT PRIMARY KEY,
    receipt TEXT NOT NULL
);

-- +goose Down
DROP TABLE managed_resource_mutations;
