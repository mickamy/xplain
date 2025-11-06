SELECT ctid, aid, bid, abalance
FROM pgbench_accounts
WHERE aid IN (
  SELECT aid
  FROM pgbench_accounts AS inner_accounts
  WHERE bid = 1
  ORDER BY abalance DESC
  LIMIT 500
);
