DROP TABLE order_event;
DROP INDEX idx_order_idempotency_key;
ALTER TABLE "order"
    DROP COLUMN idempotency_hash,
    DROP COLUMN idempotency_key,
    DROP COLUMN state_version;
