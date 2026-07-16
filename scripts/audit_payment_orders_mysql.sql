-- Read-only payment integrity audit for MySQL.
-- Run after migration 02135 and reconcile every returned row with the
-- corresponding payment provider. This script never modifies order data.

-- A provider trade number must not pay more than one local order.
SELECT trade_no, COUNT(*) AS order_count, GROUP_CONCAT(order_no ORDER BY order_no) AS order_numbers
FROM `order`
WHERE trade_no IS NOT NULL AND trade_no <> ''
GROUP BY trade_no
HAVING COUNT(*) > 1;

-- Paid/finished external orders without a provider transaction reference.
SELECT id, order_no, payment_id, method, amount, status, created_at, updated_at
FROM `order`
WHERE status IN (2, 5)
  AND method <> 'balance'
  AND (trade_no IS NULL OR trade_no = '')
ORDER BY created_at DESC;

-- Orders whose recorded payment method no longer matches their payment row.
SELECT o.id, o.order_no, o.payment_id, o.method AS order_platform,
       p.platform AS configured_platform, o.status, o.created_at
FROM `order` AS o
LEFT JOIN payment AS p ON p.id = o.payment_id
WHERE o.method <> 'balance'
  AND (p.id IS NULL OR o.method <> p.platform)
ORDER BY o.created_at DESC;

-- Existing external orders without an immutable gateway amount snapshot.
-- Pending rows need checkout restarted after upgrade. Paid/finished rows need
-- provider-side reconciliation because legacy callbacks did not verify amount.
SELECT id, order_no, payment_id, method, amount, trade_no, status, created_at
FROM `order`
WHERE method <> 'balance'
  AND (payment_currency IS NULL OR payment_currency = '')
ORDER BY created_at DESC;

-- Paid orders that have remained unactivated for more than ten minutes.
SELECT id, order_no, payment_id, method, amount, trade_no, updated_at
FROM `order`
WHERE status = 2
  AND updated_at < NOW() - INTERVAL 10 MINUTE
ORDER BY updated_at;
