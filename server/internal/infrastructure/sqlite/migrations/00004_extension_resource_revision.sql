-- +goose Up
-- The conditional-write revision belongs to the extension resource root so
-- labels can be versioned independently of managed state. Preserve existing
-- fences when moving them from the managed child.
ALTER TABLE extension_resources
    ADD COLUMN revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0);

UPDATE extension_resources
SET revision = (SELECT m.update_revision FROM extension_resource_managed m
                WHERE m.extension_resource_uid = extension_resources.uid)
WHERE uid IN (SELECT extension_resource_uid FROM extension_resource_managed);

ALTER TABLE extension_resource_managed DROP COLUMN update_revision;

-- +goose Down
ALTER TABLE extension_resource_managed
    ADD COLUMN update_revision INTEGER NOT NULL DEFAULT 0 CHECK (update_revision >= 0);

UPDATE extension_resource_managed
SET update_revision = (SELECT er.revision FROM extension_resources er
                       WHERE er.uid = extension_resource_managed.extension_resource_uid);

ALTER TABLE extension_resources DROP COLUMN revision;
