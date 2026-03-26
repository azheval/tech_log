# techlog-stat

[![Build](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml/badge.svg)](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml)

`techlog-stat` — автономная CLI-утилита на Go для чтения технологических журналов 1С и записи агрегированных отчетов в файлы.

## Языки

- English: [README.md](c:/ws/tech_log/go/techlog-stat/README.md)
- Русский: `README.ru.md`
- Беларуская: [README.be.md](c:/ws/tech_log/go/techlog-stat/README.be.md)

## Документация Версии

- Подробная спецификация v1 на русском: [docs/techlog-stat-v1.ru.md](c:/ws/tech_log/go/techlog-stat/docs/techlog-stat-v1.ru.md)
- English version: [docs/techlog-stat-v1.en.md](c:/ws/tech_log/go/techlog-stat/docs/techlog-stat-v1.en.md)
- Беларуская версія: [docs/techlog-stat-v1.be.md](c:/ws/tech_log/go/techlog-stat/docs/techlog-stat-v1.be.md)

## Текущие Отчеты

Поддерживаются отчеты:

- `sdbl-context`
- `call-context`
- `dbmssql-context`
- `postgres-context` или `dbpostgrs-context`
- `file-context` или `dbv8dbeng-context`
- `lock-context` или `locks-context`
- `timeout-context`
- `deadlock-context`
- `error-descr` или `excp-descr`

Контекстные отчеты:

- читают `*.log` напрямую
- агрегируют длительность и количество по `Context`
- пишут `summary.txt`, `contexts.csv`, `run.json`, `errors.log`

Отчеты по ошибкам:

- читают `*.log` напрямую
- агрегируют `EXCP` и `QERR` по нормализованному `Descr`
- пишут `summary.txt`, `errors.csv`, `run.json`, `errors.log`

## Пример

```bash
./techlog-stat.exe call-context --input C:/v8/logs --glob "rphost_*/*.log" --output C:/reports/call_2026-03-24 --top 10 --workers 10 --format text --filter Usr=DefUser --filter DataBase=conf_null --duration 1s
```

## Примечания

События базы данных сопоставляются так:

- `dbmssql-context` -> `DBMSSQL`
- `postgres-context` / `dbpostgrs-context` -> `DBPOSTGRS`
- `file-context` / `dbv8dbeng-context` -> `DBV8DBEng`

События блокировок сопоставляются так:

- `lock-context` / `locks-context` -> `TLOCK`, `TTIMEOUT`, `TDEADLOCK`
- `timeout-context` -> `TTIMEOUT`
- `deadlock-context` -> `TDEADLOCK`

События ошибок сопоставляются так:

- `error-descr` / `excp-descr` -> `EXCP`, `QERR`
- описания нормализуются по аналогии с legacy Perl-скриптом:
  - IPv6 endpoints -> `{IPV6}`
  - IPv4 endpoints -> `{IPV4}`
  - UUIDs -> `{UUID}`
  - фрагменты вида `начат: dd.mm.yyyy в hh:mm:ss` -> `{DtTm}`
