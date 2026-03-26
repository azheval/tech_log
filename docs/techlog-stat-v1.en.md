# techlog-stat v1

## CLI Contract

```text
techlog-stat <report> --input <dir> --glob <pattern> --output <dir> [--top N] [--format list] [--workers N] [--filter key=value] [--duration value]
```

## Flags

- `--input`
  Root directory for log discovery.
- `--glob`
  File mask relative to `--input`, for example `*/*.log` or `rphost_*/*.*`.
  Supported wildcards: `*` and `?`.
  Path separator inside the pattern must be `/`.
  Recursive `**` is not supported.
- `--output`
  Output directory for a single report run.
- `--top`
  Maximum number of ranked rows, default `100`.
- `--format`
  Comma-separated formats: `text`, `csv`, `json`.
- `--workers`
  File-level parallelism, default `1`.
- `--filter`
  Raw event filter in `key=value` form.
  The flag can be repeated.
  All filters are combined with `AND`.
  Missing fields do not cause errors, but the event does not match.
- `--duration`
  Minimum raw event duration filter.
  The filter is applied before aggregation.
  1C stores duration in microseconds, but the CLI accepts a human-friendly threshold.
  A bare number means seconds: `--duration 5` equals `5s`.
  Supported suffixes include `us`, `ms`, `s`, and `m`.

## Output Contract

Each run writes a dedicated output directory:

```text
<output>/
  summary.txt
  contexts.csv or errors.csv
  run.json
  errors.log
```

## summary.txt

Human-readable report with:

- report name
- generation timestamps
- input parameters
- file counters
- total event count
- total duration
- average duration
- ranked rows

## CSV Output

Context reports write `contexts.csv`:

```text
rank,context,short_context,total_duration_ms,time_pct,count,count_pct,avg_duration_ms
```

Error reports write `errors.csv`:

```text
rank,event,description,short_description,total_duration_ms,time_pct,count,count_pct,avg_duration_ms
```

## run.json

Execution metadata includes:

- tool version
- report kind
- timestamps
- run duration
- input and output paths
- file counters
- bytes read
- selected options

## errors.log

Per-file processing failures and parser anomalies that do not stop the whole run.
