# techlog-stat

[![Build](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml/badge.svg)](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml)

`techlog-stat` is a standalone Go CLI for reading 1C technological logs and writing aggregate reports to files.

## Current reports

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

Context reports:

- read `*.log` files directly
- aggregate duration and count by `Context`
- write `summary.txt`, `contexts.csv`, `run.json`, `errors.log`

Error reports:

- read `*.log` files directly
- aggregate `EXCP` and `QERR` by normalized `Descr`
- write `summary.txt`, `errors.csv`, `run.json`, `errors.log`

## Example

```bash
./techlog-stat.exe call-context --input C:/v8/logs --glob "rphost_*/*.log" --output C:/reports/call_2026-03-24 --top 10 --workers 10 --format text
```

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
