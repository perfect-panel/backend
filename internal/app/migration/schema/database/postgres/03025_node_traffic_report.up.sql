CREATE TABLE node_traffic_report (
    id BIGSERIAL PRIMARY KEY,
    report_key VARCHAR(64) NOT NULL UNIQUE,
    digest VARCHAR(64) NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    payload BYTEA,
    usage_applied BOOLEAN NOT NULL DEFAULT FALSE,
    completed BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_traffic_report_pending ON node_traffic_report (completed, id);
