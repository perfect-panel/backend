ALTER TABLE `order`
    ADD COLUMN `state_version` BIGINT NOT NULL DEFAULT 0 COMMENT 'Monotonic version for order state transitions' AFTER `status`,
    ADD COLUMN `idempotency_key` VARCHAR(128) NULL COMMENT 'V2 create idempotency key' AFTER `guest_checkout_token_hash`,
    ADD COLUMN `idempotency_hash` CHAR(64) NULL COMMENT 'Stable hash of V2 create request' AFTER `idempotency_key`;

CREATE UNIQUE INDEX `idx_order_idempotency_key` ON `order` (`idempotency_key`);

CREATE TABLE `order_event` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `order_id` BIGINT NOT NULL,
    `order_no` VARCHAR(255) NOT NULL,
    `event_type` VARCHAR(64) NOT NULL,
    `payload` TEXT NOT NULL,
    `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `published_at` DATETIME(3) NULL DEFAULT NULL,
    PRIMARY KEY (`id`),
    KEY `idx_order_event_order_id_id` (`order_id`, `id`),
    KEY `idx_order_event_order_no_id` (`order_no`, `id`),
    KEY `idx_order_event_published_at_id` (`published_at`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
