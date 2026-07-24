-- MySQL unique indexes already permit multiple NULL values.  Normalize rows
-- written by the affected release; the repository fix prevents new empty keys.
UPDATE `order` SET `idempotency_key` = NULL WHERE `idempotency_key` = '';
UPDATE `order` SET `idempotency_hash` = NULL WHERE TRIM(`idempotency_hash`) = '';
