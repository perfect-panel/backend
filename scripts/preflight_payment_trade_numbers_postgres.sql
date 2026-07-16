-- Run before migration 02135. Any returned row must be reconciled and fixed
-- before the unique provider transaction index can be installed.
SELECT trade_no, COUNT(*) AS order_count, STRING_AGG(order_no, ',' ORDER BY order_no) AS order_numbers
FROM "order"
WHERE trade_no IS NOT NULL AND trade_no <> ''
GROUP BY trade_no
HAVING COUNT(*) > 1;
