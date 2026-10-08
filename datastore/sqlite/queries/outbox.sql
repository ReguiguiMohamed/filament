-- name: InsertOutbox :one
INSERT INTO outbox(tenant_id,id,destination,ordering_key,kind,payload_version,payload,created_at)
VALUES(sqlc.arg(tenant_id),sqlc.arg(id),sqlc.arg(destination),sqlc.arg(ordering_key),sqlc.arg(kind),sqlc.arg(payload_version),sqlc.arg(payload),sqlc.arg(created_at))
ON CONFLICT(tenant_id,id) DO UPDATE SET id=excluded.id
WHERE outbox.destination=excluded.destination AND outbox.ordering_key=excluded.ordering_key AND outbox.kind=excluded.kind
AND outbox.payload_version=excluded.payload_version AND outbox.payload=excluded.payload AND outbox.created_at=excluded.created_at
RETURNING id;

-- name: ClaimOutbox :one
UPDATE outbox SET claim_token=sqlc.arg(claim_token),claim_expires_at=CAST(unixepoch('subsec') * 1000 AS INTEGER)+CAST(sqlc.arg(ttl_ms) AS BIGINT),attempt_count=attempt_count+1
WHERE sequence IN (SELECT e.sequence FROM outbox e
 WHERE e.destination=sqlc.arg(destination) AND e.published_at=0 AND e.next_attempt_at<=CAST(unixepoch('subsec') * 1000 AS INTEGER) AND e.claim_expires_at<=CAST(unixepoch('subsec') * 1000 AS INTEGER)
 AND (e.ordering_key='' OR NOT EXISTS (SELECT 1 FROM outbox prior
  WHERE prior.tenant_id=e.tenant_id AND prior.destination=e.destination AND prior.ordering_key=e.ordering_key
  AND prior.published_at=0 AND prior.sequence<e.sequence))
 ORDER BY e.sequence LIMIT 1) RETURNING tenant_id,id,destination,ordering_key,kind,payload_version,payload,created_at,claim_token,attempt_count;

-- name: CompleteOutbox :execrows
UPDATE outbox SET published_at=CAST(unixepoch('subsec') * 1000 AS INTEGER),claim_token='',claim_expires_at=0,last_error_code=''
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND claim_token=sqlc.arg(claim_token)
AND published_at=0 AND claim_expires_at>CAST(unixepoch('subsec') * 1000 AS INTEGER);

-- name: RetryOutbox :execrows
UPDATE outbox SET next_attempt_at=CAST(unixepoch('subsec') * 1000 AS INTEGER)+CAST(sqlc.arg(delay_ms) AS BIGINT),last_error_code=sqlc.arg(error_code),claim_token='',claim_expires_at=0
WHERE tenant_id=sqlc.arg(tenant_id) AND id=sqlc.arg(id) AND claim_token=sqlc.arg(claim_token)
AND published_at=0 AND claim_expires_at>CAST(unixepoch('subsec') * 1000 AS INTEGER);
