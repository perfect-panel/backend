DROP INDEX IF EXISTS "uniq_order_trade_no";

ALTER TABLE "order"
    DROP COLUMN "payment_currency",
    DROP COLUMN "payment_amount";
