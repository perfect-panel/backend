UPDATE "order"
SET "trade_no" = NULL
WHERE "trade_no" = '';

ALTER TABLE "order"
    ADD COLUMN "payment_amount" bigint NOT NULL DEFAULT 0,
    ADD COLUMN "payment_currency" varchar(16) NOT NULL DEFAULT '';

CREATE UNIQUE INDEX "uniq_order_trade_no"
    ON "order" ("trade_no")
    WHERE "trade_no" IS NOT NULL AND "trade_no" <> '';
