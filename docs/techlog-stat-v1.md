# techlog-stat v1

## CLI contract

```text
techlog-stat sdbl-context --input <dir> --glob <pattern> --output <dir> [--top N] [--format list] [--workers N]
```

### Flags

- `--input`
  root directory for log discovery
- `--glob`
  file mask relative to `--input`, for example `*/*.log` or `rphost_*/*.*`
  supported wildcards: `*` and `?`
  path separator inside the pattern must be `/`
  recursive `**` is not supported
- `--output`
  output directory for a single report run
- `--top`
  max number of rows in ranked outputs, default `100`
- `--format`
  comma-separated formats: `text`, `csv`, `json`
- `--workers`
  file-level parallelism, default `1`

## Output contract

Each run writes a dedicated output directory:

```text
<output>\
  summary.txt
  contexts.csv
  run.json
  errors.log
```

### summary.txt

Human-readable report:

- report name
- generation timestamps
- input parameters
- file counters
- total event count
- total duration
- average duration
- top contexts by total duration

### contexts.csv

Main tabular output for analysis tools.

Columns:

```text
rank,context,total_duration_ms,time_pct,count,count_pct,avg_duration_ms
```

### run.json

Execution metadata:

- tool version
- report kind
- timestamps
- run duration
- input and output paths
- file counters
- bytes read
- selected options

### errors.log

Per-file processing failures and parser anomalies that do not stop the whole run.

## Non-functional constraints

- streaming only, no loading full files into memory
- path metadata should be parsed once per file
- parser should avoid regex-heavy processing on every line where possible
- report aggregation should happen in the same pass as event parsing
- default behavior should be conservative and stable on one disk: `workers=1`

## Internal architecture

### Discovery

- discover input files from `input + glob`
- sort files for stable runs

### Parsing

- read file line by line with a large buffered reader
- detect start of a new event by log header signature
- keep minimal parser state for multiline events
- assemble only the current event payload

### Aggregation

- accumulate totals for:
  - total duration
  - total count
  - per-context duration
  - per-context count

### Rendering

- convert internal aggregates into ranked report rows
- write file outputs through dedicated writer implementations
