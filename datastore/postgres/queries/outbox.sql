-- name: InsertOutbox :one
INSERT INTO outbox(tenant_id,id,destination,ordering_key,kind,payload_version,payload,created_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(id),sqlc.arg(destination),sqlc.arg(ordering_key),sqlc.arg(kind),sqlc.arg(payload_version),sqlc.arg(payload),sqlc.arg(created_at))
ON CONFLICT(tenant_id,id) DO UPDATE SET id=excluded.id
WHERE outbox.destination=excluded.destination AND outbox.ordering_key=excluded.ordering_key AND outbox.kind=excluded.kind
AND outbox.payload_version=excluded.payload_version AND outbox.payload=excluded.payload AND outbox.created_at=excluded.created_at
RETURNING id;

-- name: ClaimOutbox :one
WITH candidate AS (SELECT e.sequence FROM outbox e
 WHERE e.destination=sqlc.arg(destination) AND e.published_at=0 AND e.next_attempt_at<=CAST(extract(epoch FROM clock_timestamp()) * 1000 AS BIGINT) AND e.claim_expires_at<=CAST(extract(epoch FROM clock_timestamp()) * 1000 AS BIGINT)
 AND (e.ordering_key='' OR NOT EXISTS (SELECT 1 FROM outbox prior
  WHERE prior.tenant_id=e.tenant_id AND prior.destination=e.destination AND prior.ordering_key=e.ordering_key
  AND prior.published_at=0 AND prior.sequence<e.sequence))
 ORDER BY e.sequence LIMIT 1 FOR UPDATE OF e SKIP LOCKED)
UPDATE outbox o SET claim_token=sqlc.arg(claim_token),claim_expires_at=CAST(extract(epoch FROM clock_timestamp()) * 1000 AS BIGINT)+CAST(sqlc.arg(ttl_ms) AS BIGINT),attempt_count=o.attempt_count+1
FROM candidate c WHERE o.sequence=c.sequence
RETURNING o.tenant_id,o.id,o.destination,o.ordering_key,o.kind,o.payload_version,o.payload,o.created_at,o.claim_token,o.attempt_count;

-- name: CompleteOutbox :execrows
UPDATE outbox SET published_at=CAST(extract(epoch FROM clock_timestamp()) * 1000 AS BIGINT),claim_token='',claim_expires_at=0,last_error_code=''
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND claim_token=sqlc.arg(claim_token)
AND published_at=0 AND claim_expires_at>CAST(extract(epoch FROM clock_timestamp()) * 1000 AS BIGINT);

-- name: RetryOutbox :execrows
UPDATE outbox SET next_attempt_at=CAST(extract(epoch FROM clock_timestamp()) * 1000 AS BIGINT)+CAST(sqlc.arg(delay_ms) AS BIGINT),last_error_code=sqlc.arg(error_code),claim_token='',claim_expires_at=0
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND claim_token=sqlc.arg(claim_token)
AND published_at=0 AND claim_expires_at>CAST(extract(epoch FROM clock_timestamp()) * 1000 AS BIGINT);

-- name: LockOutboxTenant :one
SELECT id FROM tenants WHERE id=sqlc.arg(tenant_id) FOR NO KEY UPDATE;
