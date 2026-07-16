UPDATE `order`
SET `trade_no` = NULL
WHERE `trade_no` = '';

ALTER TABLE `order`
    ADD COLUMN `trade_no_unique` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin
        GENERATED ALWAYS AS (NULLIF(`trade_no`, '')) STORED,
    ADD UNIQUE INDEX `uniq_order_trade_no` (`trade_no_unique`);

ALTER TABLE `order`
    ADD COLUMN `payment_amount` bigint NOT NULL DEFAULT 0 COMMENT 'Amount requested by payment gateway in minor units' AFTER `fee_amount`,
    ADD COLUMN `payment_currency` varchar(16) NOT NULL DEFAULT '' COMMENT 'Payment gateway currency' AFTER `payment_amount`;
