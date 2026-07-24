-- Generic order updates in v1.14.20 could rewrite NULL idempotency values as
-- empty strings.  Remove the bad values before replacing the index so existing
-- orders no longer conflict with each other.
UPDATE "order" SET idempotency_key = NULL WHERE idempotency_key = '';
UPDATE "order" SET idempotency_hash = NULL WHERE BTRIM(idempotency_hash) = '';

DROP INDEX IF EXISTS idx_order_idempotency_key;
CREATE UNIQUE INDEX idx_order_idempotency_key ON "order" (idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';
