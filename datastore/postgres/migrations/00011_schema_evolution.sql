-- +goose Up
-- Exact versioned payload bytes are authoritative; indexed columns are checked
-- against reducer reconstruction on load. No runner/checkpoint admission changes.
CREATE TABLE schema_states (
 tenant_id UUID NOT NULL REFERENCES tenants(id),
 scope_id TEXT NOT NULL,
 pipeline_id UUID NOT NULL,
 route_id TEXT NOT NULL,
 contract_id TEXT NOT NULL,
 revision BIGINT NOT NULL CHECK (revision > 0),
 payload_version BIGINT NOT NULL CHECK (payload_version = 1),
 initial_bytes BYTEA NOT NULL,
 bindings_bytes BYTEA NOT NULL,
 pending_operation_id TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT (clock_timestamp()),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT (clock_timestamp()),
 PRIMARY KEY (tenant_id, scope_id),
 UNIQUE (tenant_id, pipeline_id, route_id, contract_id),
 FOREIGN KEY (pipeline_id, tenant_id) REFERENCES pipelines(id, tenant_id)
);
CREATE TABLE schema_versions (
 tenant_id UUID NOT NULL,
 scope_id TEXT NOT NULL,
 version_id TEXT NOT NULL,
 resource TEXT NOT NULL,
 previous_version_id TEXT NOT NULL DEFAULT '',
 encoding_version BIGINT NOT NULL CHECK (encoding_version = 1),
 fingerprint BYTEA NOT NULL CHECK (length(fingerprint) = 32),
 model_bytes BYTEA NOT NULL,
 record_bytes BYTEA NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT (clock_timestamp()),
 PRIMARY KEY (tenant_id, scope_id, version_id),
 FOREIGN KEY (tenant_id, scope_id) REFERENCES schema_states(tenant_id, scope_id)
);
CREATE TABLE schema_operations (
 tenant_id UUID NOT NULL,
 scope_id TEXT NOT NULL,
 operation_id TEXT NOT NULL,
 sequence BIGINT NOT NULL CHECK (sequence > 0),
 revision BIGINT NOT NULL CHECK (revision > 0),
 phase TEXT NOT NULL CHECK (phase IN ('planned','applying','verified','activated','blocked')),
 payload_version BIGINT NOT NULL CHECK (payload_version = 1),
 record_bytes BYTEA NOT NULL,
 activated_revision BIGINT NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT (clock_timestamp()),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT (clock_timestamp()),
 activated_at TIMESTAMPTZ,
 PRIMARY KEY (tenant_id, scope_id, operation_id),
 UNIQUE (tenant_id, scope_id, sequence),
 CHECK ((phase = 'activated') = (activated_revision > 0)),
 FOREIGN KEY (tenant_id, scope_id) REFERENCES schema_states(tenant_id, scope_id)
);
CREATE INDEX schema_operations_pending_idx ON schema_operations(tenant_id, phase) WHERE phase <> 'activated';
CREATE TABLE schema_binding_revisions (
 tenant_id UUID NOT NULL,
 scope_id TEXT NOT NULL,
 revision BIGINT NOT NULL CHECK (revision > 0),
 payload_version BIGINT NOT NULL CHECK (payload_version = 1),
 bindings_bytes BYTEA NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT (clock_timestamp()),
 PRIMARY KEY (tenant_id, scope_id, revision),
 FOREIGN KEY (tenant_id, scope_id) REFERENCES schema_states(tenant_id, scope_id)
);

-- +goose Down
DROP TABLE schema_binding_revisions;
DROP TABLE schema_operations;
DROP TABLE schema_versions;
DROP TABLE schema_states;
