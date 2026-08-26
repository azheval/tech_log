# techlog-stat v2

`techlog-stat` reads 1C technological logs and writes offline reports. Version 2 adds a single-pass `analyze` command and period-to-period `compare`.

## Architecture

```text
matching files
  -> internal/techlog streaming parser
  -> ordered event consumer
       -> overview aggregates and time buckets
       -> SQL/SDBL fingerprints
       -> CALL/SCALL traces and error context
       -> lock aggregates and samples
       -> web, lifecycle, process, license, and file-database aggregates
  -> text / CSV / JSON / self-contained HTML
```

Files may be parsed concurrently with `--workers`; stateful analysis is consumed in discovery order. Each parsed event is filtered before it enters every collector, so overview totals, SQL, traces, and locks describe the same population.

The parser and collectors operate in one pass. File parsing can run concurrently, while the ordered consumer receives bounded batches and preserves deterministic stateful correlation. Retained investigation samples, trace state, pending error contexts, and HTML previews are bounded.

## Event coverage and conservative correlation

`analyze` adds these event-specific sections in addition to the overview, SQL, locks, and CALL traces:

| Events | Analysis |
| --- | --- |
| `SCALL` | Server-call aggregates by `Interface`, `IName`, `Method`, context, user, database, and process, with duration and optional resource metrics plus bounded slow samples. |
| `VRSREQUEST`, `VRSRESPONSE`, `VRSCACHE` | Web request/response groups, normalized URI, status/result, bytes, cache hit/miss, and bounded slow/error samples. A response or cache event is linked only when there is exactly one compatible candidate on its source/process/thread lane with non-conflicting identifiers. |
| `SESN`, `CONN` | Explicit start/open and finish/close lifecycle pairs identified by an explicit ID. Attach-like records and missing IDs do not create inferred lifecycles. |
| `PROC`, `SCOM` | Process and SCOM aggregates, bounded slow PROC samples, and only explicitly parsed SCOM process relations. |
| `LIC`, `HASP` | License acquisition/release/failure classes, HASP origin and safe system summaries. License paths, serial-like values, and MAC addresses are redacted in retained samples. |
| `EXCPCNTX`, `EXCP`, `QERR` | Error groups and bounded raw error/context samples. An `EXCPCNTX` record enriches only one nearest compatible error in the configured window; ambiguous or unmatched contexts remain visible as quality outcomes. |
| `DBV8DBEng` | File-database aggregates by function, table, category, file, database, process, user, and conservative operation class. `Rows` and `RowsAffected` preserve missing-vs-zero; absolute file paths are redacted. |

URI normalization removes query values, sorts query names, and replaces numeric or UUID-shaped path segments. These transformations reduce accidental disclosure and make groups more stable; they are not a security boundary for arbitrary raw events.

## Commands

## Local web interface: `serve`

Start the embedded local frontend with an allowed log root:

```powershell
./techlog-stat.exe serve --input C:/v8/logs --listen 127.0.0.1:8080
```

The frontend is self-contained in the executable and has no CDN or external assets. It starts analysis runs, displays status and progress, and can request their cancellation. A run may select a directory only within the configured `--input` root and applies a glob relative to that directory, together with analysis options and filters.

The Overview, Events, SQL, Traces, Locks, and Source tabs support filtering and drill-down to source events. Source drill-down re-reads only the immutable set of files matched by that run, never a browser-provided path or glob. The UI compares any two completed in-memory runs and renders CSV, JSON, and standalone HTML downloads for a selected run.

`--listen` accepts loopback addresses only, so this is a local interface rather than a network service. Results live only in memory and are lost on process shutdown. Retention is bounded by `--max-runs` (default `8`); terminal runs can be removed to free capacity, and `--max-concurrent` (default `1`) bounds simultaneous analyses.

### Analyze

```bash
./techlog-stat.exe analyze \
  --input C:/v8/logs --glob "rphost_*/*.log" \
  --output C:/reports/overview --bucket 1m --workers 8 --top 100 \
  --format text,csv,json,html \
  --filter Usr=DefUser --filter DataBase=conf_null \
  --duration 500ms --date-from 2026-08-23 --date-to 2026-08-23 \
  --time-from 09:00 --time-to 18:00
```

Important options:

| Option | Meaning |
| --- | --- |
| `--glob` | Mask relative to `--input`; default `*/*.log`. |
| `--bucket` | Time-series bucket for `analyze`; default `1m`. Go duration syntax is used, for example `5m` or `1h`. |
| `--top` | Number of longest raw events retained in the overview; default `100`. |
| `--workers` | Maximum file parsing parallelism; default `1`. |
| `--filter key=value` | Exact field match; repeat to combine conditions with AND. |
| `--duration` | Minimum event duration. A bare number means seconds; `500ms` and `5s` are also valid. |
| `--date-from` / `--date-to` | Inclusive `YYYY-MM-DD` range. |
| `--time-from` / `--time-to` | Time-of-day range, `HH:MM[:SS]`; an overnight range is supported. |

The parser still records parse-quality counters for excluded records, while all analytical sections include only records that pass the filters.

### Compare

Create two `analyze` JSON reports first, then run:

```bash
./techlog-stat.exe compare \
  --baseline C:/reports/before/run.json \
  --current C:/reports/after/run.json \
  --output C:/reports/compare \
  --threshold-pct 5 --threshold-abs-us 1000 \
  --format text,csv,json,html
```

`--threshold-pct` defaults to `5`; `--threshold-abs-us` defaults to `0`. Comparison covers totals, event types, users, databases, processes, SQL fingerprints, SCALL `ByCall`, web requests, SESN/CONN `ByEvent`, `PROCByProcess`, `SCOMByOperation`, `LIC`, and DBV8DBEng `ByFunc`. `EXCPCNTX` groups are also compared, but count-only: they never produce a performance-regression classification.

## Analyze output

Only files selected by `--format` are written, except `errors.log`, which is always written.

| File | Format | Contents |
| --- | --- | --- |
| `summary.txt` | text | Totals, event types, bounded Top SQL, and trace quality. |
| `event_types.csv` | CSV | One aggregate row per technological-log event name. |
| `sql.csv` | CSV | Literal-independent SQL/SDBL fingerprints, durations, row counts, contexts, users, and databases. |
| `traces.csv` | CSV | One row per trace span, with trace identifiers, event fields, and source text. |
| `locks.csv` | CSV | Lock event/context/table/region aggregates, conflicts, relations, and samples. |
| `scall.csv` | CSV | SCALL call groups and bounded samples. |
| `web.csv` | CSV | VRS request/response and cache aggregates and samples. |
| `sessions.csv` | CSV | Completed and incomplete SESN/CONN lifecycle records. |
| `processes.csv` | CSV | PROC/SCOM aggregates, explicit relations, and bounded samples. |
| `licenses.csv` | CSV | LIC/HASP aggregates, safe system summaries, and redacted samples. |
| `filedb.csv` | CSV | DBV8DBEng aggregates and redacted file-database samples. |
| `error_contexts.csv` | CSV | EXCP/QERR groups plus matched and orphan EXCPCNTX samples. |
| `run.json` | JSON | Full `OverviewResult`, including the event-specific sections and their quality counters. |
| `report.html` | HTML | Portable dashboard with embedded CSS/JavaScript, no CDN dependency, and bounded panels for available event sections. |
| `errors.log` | text | File-level discovery or parsing errors. |

All CSV files have a UTF-8 BOM for spreadsheet compatibility. Durations in overview JSON and overview CSV analytical columns are microseconds unless a column explicitly says otherwise.

### HTML dashboard

The HTML report is designed to be opened directly from disk. It includes:

- a canvas time-series chart;
- navigation between overview, SQL/SDBL, locks, traces, and dimensions;
- client-side table search, sortable headers, and an `(unknown)` dimension toggle;
- expandable trace and lock samples; and
- local browser persistence for selected tab and simple UI filters.

HTML renders bounded detail to protect browsers: at most 100 SQL rows, 100 traces, 100 spans per displayed trace, 100 lock rows per section, and 4,000 runes for each raw-event preview. The complete retained data remains in JSON and CSV.

## Compare output

`compare` writes `summary.txt`, `compare.csv`, `run.json`, and `report.html` when their formats are selected. Its specialized rows include SCALL `ByCall`, web requests, SESN/CONN `ByEvent`, `PROCByProcess`, `SCOMByOperation`, `LIC`, EXCPCNTX groups, and DBV8DBEng `ByFunc`, alongside the overview and SQL comparisons. EXCPCNTX rows compare count only, so an increase is evidence for investigation rather than a performance-regression result.

## Existing report commands

The original `sdbl-context`, `call-context`, database, lock, timeout, deadlock, and error-description commands remain supported. Their aggregate mode writes a summary, a report-specific CSV, JSON, and `errors.log`; `--mode raw` writes the top N events per hour.

## Limits and interpretation

- A log file name must begin with `YYMMDDHH`; invalid names are reported per file and do not stop the whole run.
- `--filter` is an exact string comparison, not a regular expression or substring search.
- SQL normalization replaces numeric and quoted-string literals. It is a fingerprinting aid, not a SQL parser or execution-plan analysis.
- Trace correlation uses source, process, OS thread, and optional client/call/transaction identifiers. VRS and error-context joins require a unique compatible candidate; ambiguous records are not guessed. Quality counters expose orphan, ambiguity, and retention outcomes.
- Trace, lifecycle, web, error-context, lock, and investigation-sample collectors use bounded retention. Inspect CSV/JSON rather than assuming an HTML preview is complete.
- The tool analyses files available at run time. It is not a background monitoring or alerting service.

## Collection guidance

For 1C guidance on preparing data for an investigation, see [Recommendations for collecting data to investigate technological problems](https://its.1c.ru/db/metod8dev/content/6005/hdoc).
