# techlog-stat v1

## CLI Contract

```text
techlog-stat <report> --input <dir> --glob <pattern> --output <dir> [--mode aggregate|raw] [--top N] [--format list] [--workers N] [--filter key=value] [--duration value] [--date-from YYYY-MM-DD] [--date-to YYYY-MM-DD] [--time-from HH:MM[:SS]] [--time-to HH:MM[:SS]]
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
- `--mode`
  Output mode.
  Supported values:
  - `aggregate` for ranked grouped statistics.
  - `raw` for top individual events per hour without aggregation.
- `--top`
  Maximum number of ranked rows, default `100`.
  In raw mode the value is applied separately to each hour bucket.
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
  The filter is applied before aggregation or raw ranking.
  1C stores duration in microseconds, but the CLI accepts a human-friendly threshold.
  A bare number means seconds: `--duration 5` equals `5s`.
  Supported suffixes include `us`, `ms`, `s`, and `m`.
- `--date-from`
  Inclusive lower date bound in `YYYY-MM-DD`.
- `--date-to`
  Inclusive upper date bound in `YYYY-MM-DD`.
- `--time-from`
  Lower time-of-day bound in `HH:MM` or `HH:MM:SS`.
- `--time-to`
  Upper time-of-day bound in `HH:MM` or `HH:MM:SS`.

## Output Contract

Each run writes a dedicated output directory.

Aggregate mode:

```text
<output>/
  summary.txt
  contexts.csv or errors.csv
  run.json
  errors.log
```

Raw mode:

```text
<output>/
  raw.txt
  raw.csv
  raw.json
  errors.log
```

## Aggregate Output

`summary.txt` contains:

- report name
- generation timestamps
- input parameters
- file counters
- total event count
- total duration
- average duration
- ranked rows

Context reports write `contexts.csv`:

```text
rank,context,short_context,total_duration_ms,time_pct,count,count_pct,avg_duration_ms
```

Error reports write `errors.csv`:

```text
rank,event,description,short_description,total_duration_ms,time_pct,count,count_pct,avg_duration_ms
```

## Raw Output

Raw output keeps filtered individual events and sorts them by descending raw duration.

Behavior:

- events are grouped by day
- inside each day, events are grouped by hour
- `top N` is calculated independently for each hour
- timestamps are reconstructed from the log file hour and the event line time inside the file
- context reports keep both `context` and `short_context`
- error reports keep both `description` and `short_description`

`raw.csv` for context reports:

```text
date,hour,timestamp,event,file,duration_micros,duration_ms,context,short_context
```

`raw.csv` for error reports:

```text
date,hour,timestamp,event,file,duration_micros,duration_ms,description,short_description
```

`raw.json` contains the same data grouped as:

- days
- hours within a day
- raw events within an hour

## run.json

Aggregate JSON includes:

- tool version
- report kind
- timestamps
- run duration
- input and output paths
- file counters
- bytes read
- selected options
- ranked rows

Raw JSON includes the same run metadata plus grouped raw events.

## errors.log

Per-file processing failures and parser anomalies that do not stop the whole run.
