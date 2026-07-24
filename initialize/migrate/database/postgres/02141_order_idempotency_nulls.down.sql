UPDATE "order" SET idempotency_key = NULL WHERE idempotency_key = '';
DROP INDEX IF EXISTS idx_order_idempotency_key;
CREATE UNIQUE INDEX idx_order_idempotency_key ON "order" (idempotency_key);
