-- Idempotent-consumer inbox for cross-domain steps (ADR-001 step 2).  Each
-- domain marks an event as processed inside its own transaction; the primary
-- key makes at-least-once deliveries safe.  When domains become services,
-- each takes its consumer rows along as its private inbox table.
CREATE TABLE domain_event_inbox (
    consumer VARCHAR(64) NOT NULL,
    event_key VARCHAR(191) NOT NULL,
    result VARCHAR(255) NOT NULL DEFAULT '',
    processed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (consumer, event_key)
);

COMMENT ON COLUMN domain_event_inbox.consumer IS 'Consuming domain step, e.g. subscription.fulfillment';
COMMENT ON COLUMN domain_event_inbox.event_key IS 'Business key of the event, e.g. order number';
COMMENT ON COLUMN domain_event_inbox.result IS 'Optional outcome needed by later steps';
