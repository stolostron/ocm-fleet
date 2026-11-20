-- +goose Up
-- This current-state fence retires delayed Update activity attempts without a
-- mutation ledger or a synthetic desired-generation bump.
ALTER TABLE extension_resource_managed
    ADD COLUMN update_revision BIGINT NOT NULL DEFAULT 0 CHECK (update_revision >= 0);

-- +goose Down
ALTER TABLE extension_resource_managed DROP COLUMN update_revision;
