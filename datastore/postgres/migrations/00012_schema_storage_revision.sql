-- +goose Up
ALTER TABLE schema_states ADD COLUMN storage_revision BIGINT NOT NULL DEFAULT 1 CHECK (storage_revision > 0);

-- +goose Down
ALTER TABLE schema_states DROP COLUMN storage_revision;
