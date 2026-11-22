CREATE TABLE node_traffic_report (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    report_key CHAR(64) CHARACTER SET ascii NOT NULL UNIQUE,
    digest CHAR(64) CHARACTER SET ascii NOT NULL,
    received_at DATETIME(3) NOT NULL,
    payload LONGBLOB,
    usage_applied BOOLEAN NOT NULL DEFAULT FALSE,
    completed BOOLEAN NOT NULL DEFAULT FALSE,
    INDEX idx_traffic_report_pending (completed, id)
);
