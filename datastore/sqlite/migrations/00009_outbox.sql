-- +goose Up
CREATE TABLE outbox (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 tenant_id TEXT NOT NULL REFERENCES tenants(id),
 id TEXT NOT NULL,
 destination TEXT NOT NULL,
 ordering_key TEXT NOT NULL,
 kind TEXT NOT NULL,
 payload_version BIGINT NOT NULL,
 payload BLOB NOT NULL,
 created_at BIGINT NOT NULL,
 attempt_count BIGINT NOT NULL DEFAULT 0,
 next_attempt_at BIGINT NOT NULL DEFAULT 0,
 last_error_code TEXT NOT NULL DEFAULT '',
 published_at BIGINT NOT NULL DEFAULT 0,
 claim_token TEXT NOT NULL DEFAULT '',
 claim_expires_at BIGINT NOT NULL DEFAULT 0,
 UNIQUE (tenant_id,id)
);
CREATE INDEX outbox_pending ON outbox(destination,published_at,next_attempt_at,claim_expires_at);
CREATE INDEX outbox_ordering ON outbox(tenant_id,destination,ordering_key,published_at,sequence);

-- +goose Down
DROP TABLE outbox;
