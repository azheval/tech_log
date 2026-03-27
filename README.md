# techlog-stat

[![Build](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml/badge.svg)](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml)

`techlog-stat` is a standalone Go CLI for reading 1C technological logs and writing reports to files.

## Languages

- English: `README.md`
- Belarusian: [README.be.md](/README.be.md)
- Russian: [README.ru.md](/README.ru.md)

## Version Documentation

- Detailed v1 specification in English: [docs/techlog-stat-v1.en.md](/docs/techlog-stat-v1.en.md)
- Belarusian translation: [docs/techlog-stat-v1.be.md](/docs/techlog-stat-v1.be.md)
- Russian translation: [docs/techlog-stat-v1.ru.md](/docs/techlog-stat-v1.ru.md)

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
