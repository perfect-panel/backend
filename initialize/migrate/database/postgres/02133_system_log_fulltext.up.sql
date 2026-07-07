CREATE INDEX idx_content_gin ON system_logs USING GIN (to_tsvector('simple', content));
