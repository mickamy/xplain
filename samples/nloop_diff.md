# xplain diff

## Summary
- Execution: 50.860 ms → 48.031 ms (-2.829 ms, -5.6%)
- Planning: 2.127 ms → 2.786 ms (+0.659 ms, +31.0%)

### Insights
- ✅ Seq Scan · pgbench_accounts self -10.29 ms (-14.2%)

### Regressions
- None above threshold

### Improvements
| Operator | Base self (ms) | Target self (ms) | Δ self (ms) | Δ % | Rows (actual / est) |
|---|---:|---:|---:|---:|---|
| Seq Scan · pgbench_accounts | 72.37 | 62.08 | -10.29 | -14.2% | 200000 (x0.92) → 200000 (x0.92) |
