ALTER TABLE "order"
    ADD COLUMN state_version BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN idempotency_key VARCHAR(128) NULL,
    ADD COLUMN idempotency_hash CHAR(64) NULL;

CREATE UNIQUE INDEX idx_order_idempotency_key ON "order" (idempotency_key);

CREATE TABLE order_event (
    id BIGSERIAL PRIMARY KEY,
    order_id BIGINT NOT NULL,
    order_no VARCHAR(255) NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    payload TEXT NOT NULL,
    created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMP(3) NULL DEFAULT NULL
);

CREATE INDEX idx_order_event_order_id_id ON order_event (order_id, id);
CREATE INDEX idx_order_event_order_no_id ON order_event (order_no, id);
CREATE INDEX idx_order_event_published_at_id ON order_event (published_at, id);
