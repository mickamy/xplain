SELECT ctid, aid, bid, abalance
FROM pgbench_accounts
WHERE bid = 1
ORDER BY abalance DESC
LIMIT 20;
