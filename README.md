# techlog-stat

[![Build](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml/badge.svg)](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml)

`techlog-stat` is a standalone Go CLI for reading 1C technological logs and writing reports to files.

![img_001](/docs/img/001.png)
![img_002](/docs/img/002.png)

## Languages

- English: `README.md`
- Belarusian: [README.be.md](/README.be.md)
- Russian: [README.ru.md](/README.ru.md)

## Version Documentation

- Current v2 documentation in English: [docs/techlog-stat-v2.en.md](/docs/techlog-stat-v2.en.md)
- Russian translation: [docs/techlog-stat-v2.ru.md](/docs/techlog-stat-v2.ru.md)
- Belarusian translation: [docs/techlog-stat-v2.be.md](/docs/techlog-stat-v2.be.md)
- Detailed v1 specification in English: [docs/techlog-stat-v1.en.md](/docs/techlog-stat-v1.en.md)
- Belarusian translation: [docs/techlog-stat-v1.be.md](/docs/techlog-stat-v1.be.md)
- Russian translation: [docs/techlog-stat-v1.ru.md](/docs/techlog-stat-v1.ru.md)
- Current v2 guide: [English](docs/techlog-stat-v2.en.md) · [Russian](docs/techlog-stat-v2.ru.md)

## Current Reports

Supported reports:

- `sdbl-context`
- `call-context`
- `dbmssql-context`
- `postgres-context` or `dbpostgrs-context`
- `file-context` or `dbv8dbeng-context`
- `lock-context` or `locks-context`
- `timeout-context`
- `deadlock-context`
- `error-descr` or `excp-descr`
- `analyze` — single-pass overview across all event types
- `compare` — regression comparison of two `analyze` JSON reports
- `compare` — compare two `analyze` JSON reports

## Local web interface: `serve`

Start the local interface with the directory that is allowed to contain logs:

```powershell
./techlog-stat.exe serve --input C:/v8/logs --listen 127.0.0.1:8080
```

Open the displayed local address in a browser. The frontend is embedded in the executable, self-contained, and uses no CDN or other external assets. It creates analysis runs, shows their status and progress, and can request cancellation. The run form can choose a directory only within the `--input` root and a file glob relative to that directory; it also exposes analysis options and filters.

Use the Overview, Events, SQL, Traces, Locks, and Source tabs to filter result rows and drill down from retained aggregates to source events. Source-event drill-down re-reads only the files matched by that particular run, never a path or glob supplied by the browser. Two completed in-memory runs can be compared in the interface. CSV, JSON, and standalone HTML downloads are rendered from the selected run.

The server accepts loopback listen addresses only; do not use it as a network service. Runs and their results are retained in memory only and disappear when the process stops. Retention is bounded (`--max-runs`, default `8`); terminal runs may be removed to make space, and concurrent analyses are bounded by `--max-concurrent` (default `1`).

## Expanded `analyze` coverage

`analyze` consumes the selected log stream once and adds dedicated sections for:

- `SCALL` server calls, grouped by interface, object name, method, context, user, database, and process;
- `VRSREQUEST`, `VRSRESPONSE`, and `VRSCACHE` web traffic, including normalized URI, status, bytes, cache hit/miss, and bounded slow/error samples;
- `SESN` and `CONN` explicit lifecycle pairs; `PROC` and `SCOM` process activity and only explicitly parsed process relations;
- `LIC` and `HASP` licensing activity; `EXCPCNTX` context enrichment for compatible `EXCP`/`QERR` records; and expanded `DBV8DBEng` file-database aggregates.

Correlation is conservative: a VRS response or cache event is joined only when its compatible lane and identifiers produce one candidate; an `EXCPCNTX` record enriches only one nearest compatible error. Ambiguous records remain visible in quality counters instead of being guessed. Pending correlation state and retained investigation samples are bounded.

The additional sections appear in `summary.txt`, dedicated CSV files (`scall.csv`, `web.csv`, `sessions.csv`, `processes.csv`, `licenses.csv`, `filedb.csv`, `error_contexts.csv`), `run.json`, and the relevant bounded HTML panels. URI query values and identity-shaped URI path segments are normalized; license paths, serial-like values, MAC addresses, and absolute file-database paths are redacted in their respective report sections.

For log collection guidance, see 1C ITS: [Recommendations for collecting data to investigate technological problems](https://its.1c.ru/db/metod8dev/content/6005/hdoc).

## Output Modes

Aggregate mode is the default:

- context reports write `summary.txt`, `contexts.csv`, `run.json`, `errors.log`
- error reports write `summary.txt`, `errors.csv`, `run.json`, `errors.log`

Raw mode is enabled with `--mode raw`:

- keeps all current raw-event filters
- writes top N individual events per hour
- groups output by day and hour
- writes `raw.txt`, `raw.csv`, `raw.json`, `errors.log`

## Filters

Supported filters apply before aggregation or raw ranking:

For `analyze`, the same filters are applied before every collector (overview, SQL, traces, and locks). Repeated `--filter key=value` conditions are combined with AND.

- `--glob`
- `--filter key=value`
- `--duration`
- `--date-from YYYY-MM-DD`
- `--date-to YYYY-MM-DD`
- `--time-from HH:MM[:SS]`
- `--time-to HH:MM[:SS]`

## Examples

Aggregate report:

```bash
./techlog-stat.exe call-context --input C:/v8/logs --glob "rphost_*/*.log" --output C:/reports/call_2026-03-24 --top 10 --workers 10 --format text --filter Usr=DefUser --filter DataBase=conf_null --duration 1s
```

Raw top events per hour:

```bash
./techlog-stat.exe call-context --mode raw --input C:/v8/logs --glob "rphost_*/*.log" --output C:/reports/call_raw_2026-03-24 --top 10 --filter Usr=DefUser --filter DataBase=conf_null --duration 5 --date-from 2026-03-24 --date-to 2026-03-24 --time-from 09:00 --time-to 18:00
```

Overview dashboard:

```bash
./techlog-stat.exe analyze --input C:/v8/logs --glob "rphost_*/*.log" --output C:/reports/overview --bucket 1m --top 100 --workers 8 --format text,csv,json,html
```

![img_003](/docs/img/003.png)

`analyze` writes `summary.txt`, `event_types.csv`, `run.json`, `report.html`, and `errors.log` (for selected formats). The HTML dashboard is self-contained and includes a timeline chart without external CDN resources.

With `--format text,csv,json,html`, the complete `analyze` output is:

- `summary.txt` — overview plus SQL, trace, SCALL, VRS, lifecycle, process, license, error-context, and file-database summaries
- `event_types.csv`, `sql.csv`, `traces.csv`, `locks.csv`, `scall.csv`, `web.csv`, `sessions.csv`, `processes.csv`, `licenses.csv`, `filedb.csv`, `error_contexts.csv` — spreadsheet-ready sections
- `run.json` — complete overview for drill-down or `compare`, including detailed event sections and quality counters
- `report.html` — self-contained dashboard with bounded panels for available sections
- `errors.log` — per-file processing failures, if any

The dashboard works offline: it has a timeline canvas chart, report-section navigation, table search, sortable columns, an `(unknown)` toggle, expandable trace/lock details, and remembers its UI settings locally in the browser.

Compare two overview JSON files:

```bash
./techlog-stat.exe compare --baseline C:/reports/overview-before/run.json --current C:/reports/overview-after/run.json --output C:/reports/compare --threshold-pct 5 --threshold-abs-us 1000 --format text,csv,json,html
```

`compare` writes `summary.txt`, `compare.csv`, `run.json`, and `report.html`. It compares totals, event types, users, databases, processes, and SQL fingerprints, plus SCALL `ByCall`, web requests, SESN/CONN `ByEvent`, `PROCByProcess`, `SCOMByOperation`, `LIC`, `DBV8DBEng` `ByFunc`, and `EXCPCNTX` groups. EXCPCNTX groups are count-only comparisons and never produce a performance-regression classification.

## Notes

Database-related event names are mapped as follows:

- `dbmssql-context` -> `DBMSSQL`
- `postgres-context` / `dbpostgrs-context` -> `DBPOSTGRS`
- `file-context` / `dbv8dbeng-context` -> `DBV8DBEng`

Lock-related event names are mapped as follows:

- `lock-context` / `locks-context` -> `TLOCK`, `TTIMEOUT`, `TDEADLOCK`
- `timeout-context` -> `TTIMEOUT`
- `deadlock-context` -> `TDEADLOCK`

Error-related event names are mapped as follows:

- `error-descr` / `excp-descr` -> `EXCP`, `QERR`
- descriptions are normalized similarly to the legacy Perl script:
  - IPv6 endpoints -> `{IPV6}`
  - IPv4 endpoints -> `{IPV4}`
  - UUIDs -> `{UUID}`
  - `nachat: dd.mm.yyyy v hh:mm:ss` style fragments -> `{DtTm}`
