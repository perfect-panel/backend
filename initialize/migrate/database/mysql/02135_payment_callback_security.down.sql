ALTER TABLE `order`
    DROP INDEX `uniq_order_trade_no`,
    DROP COLUMN `trade_no_unique`,
    DROP COLUMN `payment_currency`,
    DROP COLUMN `payment_amount`;
