-- +goose Up
-- The conditional-write revision belongs to the extension resource root so
-- labels can be versioned independently of managed state. Preserve existing
-- fences when moving them from the managed child.
ALTER TABLE extension_resources
    ADD COLUMN revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0);

UPDATE extension_resources er
SET revision = m.update_revision
FROM extension_resource_managed m
WHERE m.extension_resource_uid = er.uid;

ALTER TABLE extension_resource_managed DROP COLUMN update_revision;

-- +goose Down
ALTER TABLE extension_resource_managed
    ADD COLUMN update_revision BIGINT NOT NULL DEFAULT 0 CHECK (update_revision >= 0);

UPDATE extension_resource_managed m
SET update_revision = er.revision
FROM extension_resources er
WHERE er.uid = m.extension_resource_uid;

ALTER TABLE extension_resources DROP COLUMN revision;
