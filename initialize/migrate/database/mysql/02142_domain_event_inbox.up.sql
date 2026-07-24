-- Idempotent-consumer inbox for cross-domain steps (ADR-001 step 2).  Each
-- domain marks an event as processed inside its own transaction; the primary
-- key makes at-least-once deliveries safe.  When domains become services,
-- each takes its consumer rows along as its private inbox table.
CREATE TABLE `domain_event_inbox` (
    `consumer` VARCHAR(64) NOT NULL COMMENT 'Consuming domain step, e.g. subscription.fulfillment',
    `event_key` VARCHAR(191) NOT NULL COMMENT 'Business key of the event, e.g. order number',
    `result` VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'Optional outcome needed by later steps',
    `processed_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`consumer`, `event_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
