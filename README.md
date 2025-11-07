# xplain

`xplain` transforms PostgreSQL `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` output into actionable insights.  
It highlights bottlenecks, surfaces skew between estimated and actual rows, and produces human-friendly reports for
terminals, HTML, and CI diff workflows.

## Features

- **Parser & model** – Reads native JSON plans and normalises them into a rich plan tree.
- **Analyzer** – Computes inclusive/exclusive timings, buffer usage, and estimation drift metrics.
- **TUI renderer** – Prints a colour-coded tree with ratio bars and warnings for hot nodes.
- **HTML renderer** – Generates a compact, shareable report with heat-mapped cards and summaries.
- **Insight engine** – Highlights hotspots, estimation drift, buffer churn, and parallel inefficiencies with quick
  remediation hints.
  - Spots issues such as nested-loop explosions, buffer churn, new temp spills, parallel worker shortfall/imbalance.
- **Diff mode** – Compares two plans and emits Markdown summaries suited for PRs/CI.
- **Runner** – Executes `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` against a PostgreSQL DSN.

## Getting Started

```bash
go install github.com/mickamy/xplain@latest
xplain version
```

> Note: The CLI requires access to PostgreSQL when using `xplain run`. Supply the connection string via `--url` or the
`DATABASE_URL` environment variable. The other commands operate on saved explain JSON files.

### 1. Capture a plan

```bash
DATABASE_URL="postgres://postgres:password@localhost:5432/bench" \
  xplain run --sql ./samples/pgbench_hot.sql \
  --out ./plans/pgbench_hot.json
```

Or capture **and** inspect in one go:

```bash
DATABASE_URL="postgres://postgres:password@localhost:5432/bench" \
  xplain analyze --sql ./samples/pgbench_hot.sql --mode tui
```

Pass `--query "SELECT ..."` if you prefer to provide SQL inline.

### 2. Inspect in the terminal

```bash
xplain report --input ./plans/pgbench_hot.json --mode tui
```

### 3. Produce an HTML report

```bash
xplain report --input ./plans/pgbench_hot.json --mode html --out report.html
```

### 4. Diff two plans

```bash
xplain diff --base ./plans/before.json \
  --target ./plans/after.json \
  --format md --out plan-regression.md
```

## Samples

The repository includes pgbench-derived examples to try locally:

- `samples/pgbench_hot.sql` / `pgbench_hot.json` — a buffer-intensive query that highlights hotspots
- `samples/pgbench_branches.sql` / `pgbench_branches.json` — a lightweight lookup over the branches table
- `samples/nested_loop_noindex.sql` / `nloop_base.json` / `nloop_index.json` — nested loop before/after adding an index
- `samples/nloop_diff.md` / `nloop_diff.json` — Markdown and JSON diff comparing those two plans
- `samples/config.example.json` — configuration template for tuning thresholds

Render it in the terminal or export HTML:

```bash
xplain report --input samples/pgbench_hot.json --mode tui
xplain report --input samples/pgbench_hot.json --mode html --out samples/pgbench_hot.html
xplain report --input samples/pgbench_branches.json --mode tui
```

Each report starts with an *Insights* block that calls out the dominant hotspots, estimator drift, buffer churn, and
parallel inefficiencies so you know where to focus first.

To regenerate the plan yourself, point `DATABASE_URL` at a pgbench instance and run:

```bash
database_url="postgres://postgres:password@localhost:5432/bench"
DATABASE_URL="$database_url" xplain run --sql samples/pgbench_hot.sql --out samples/pgbench_hot.json
DATABASE_URL="$database_url" xplain run --sql samples/pgbench_branches.sql --out samples/pgbench_branches.json

# diff the nested loop scenario once index is added
xplain diff --base samples/nloop_base.json \
  --target samples/nloop_index.json \
  --format md --min-delta 0.5 --min-percent 1 \
  --out samples/nloop_diff.md
```

### Diff Output Formats

Markdown is the default. For CI or tooling, you can request JSON:

```bash
xplain diff --base samples/nloop_base.json \
  --target samples/nloop_index.json \
  --format json --min-delta 0.5 --min-percent 1 |
  jq .
```

See `samples/nloop_diff.json` for a full example payload.

## Configuration

Thresholds used by the insight engine and diff output can be tuned via JSON configuration.

```json
{
  "insights": {
    "hotspot_critical_percent": 0.5,
    "buffer_warning_blocks": 2000,
    "nested_loop_warn_loops": 200
  },
  "diff": {
    "min_self_delta_ms": 1.0,
    "min_percent_change": 2.5
  }
}
```

Specify `--config path/to/config.json` (or set `XPLAIN_CONFIG`) to override thresholds globally. Any field you omit keeps its default value.

## Roadmap Ideas

- Enrich the analyser with pattern-based tuning hints (indexes, stats, batching).
- Deeper diff alignment for complex plan reshapes (fingerprints per subtree).
- Optional web UI with interactive sunburst/heatmap navigation.
- Exporters for JSON/CSV metrics to feed dashboards or notebooks.

## Development

```bash
go test ./...
```

You can also drive common tasks via the Makefile:

```bash
make build VERSION=0.1.0
make test
make version
```

During development you can regenerate module metadata with:

```bash
go mod tidy
```

The repository defaults to ASCII output for portability; ANSI colour can be disabled with `--color=false`.

## License

[MIT](./LICENSE)
