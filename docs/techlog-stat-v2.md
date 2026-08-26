# techlog-stat v2

Language versions:

- English: [techlog-stat-v2.en.md](/docs/techlog-stat-v2.en.md)
- Беларуская: [techlog-stat-v2.be.md](/docs/techlog-stat-v2.be.md)
- Русский: [techlog-stat-v2.ru.md](/docs/techlog-stat-v2.ru.md)

This file is the entry point for the current specification.

Version 2 documents a single-pass, bounded analysis of CALL/SCALL, VRS web
traffic, SESN/CONN lifecycles, PROC/SCOM, LIC/HASP, EXCPCNTX error context, and
DBV8DBEng file-database activity. The language guides describe conservative
correlation, redaction, report sections, and stable specialized comparisons:
SCALL ByCall, web requests, SESN/CONN ByEvent, PROCByProcess, SCOMByOperation,
LIC, EXCPCNTX groups, and DBV8DBEng ByFunc. EXCPCNTX is count-only, never a
performance-regression signal.

## Local `serve` interface

`serve --input C:/v8/logs --listen 127.0.0.1:8080` starts an embedded, self-contained browser frontend with no CDN. It manages in-memory analysis runs: directory selection is constrained to the configured input root, file selection uses a relative glob, and the UI offers run start, cancellation, status/progress, filters, SQL/traces/locks and source-event drill-down tabs, comparison of two completed runs, and CSV/JSON/HTML downloads. Source drill-down re-reads only the files matched by the selected run. The listener is loopback-only. Results are not written as durable server state: they are lost when the process stops and bounded retention (`--max-runs`) can evict terminal runs.

For 1C log collection guidance, see [Recommendations for collecting data to
investigate technological problems](https://its.1c.ru/db/metod8dev/content/6005/hdoc).
