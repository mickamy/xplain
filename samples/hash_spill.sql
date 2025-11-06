SELECT a.bid, sum(a.abalance)
FROM pgbench_accounts a
JOIN (
  SELECT bid
  FROM pgbench_branches
  WHERE bid <= 10
) b ON a.bid = b.bid
GROUP BY a.bid;
