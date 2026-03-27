# techlog-stat

[![Build](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml/badge.svg)](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml)

`techlog-stat` — автономная CLI-утилита на Go для чтения технологических журналов 1С и записи агрегированных отчетов в файлы.

## Языки

- English: [README.md](/README.md)
- Русский: `README.ru.md`
- Беларуская: [README.be.md](/README.be.md)

## Документация Версии

- Подробная спецификация v1 на русском: [docs/techlog-stat-v1.ru.md](/docs/techlog-stat-v1.ru.md)
- English version: [docs/techlog-stat-v1.en.md](/docs/techlog-stat-v1.en.md)
- Беларуская версія: [docs/techlog-stat-v1.be.md](/docs/techlog-stat-v1.be.md)

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

Агрегированный режим — по умолчанию:

- отчеты о контекстах записывают `summary.txt`, `contexts.csv`, `run.json`, `errors.log`
- отчеты об ошибках записывают `summary.txt`, `errors.csv`, `run.json`, `errors.log`

Необработанный режим включается с помощью `--mode raw`:

- сохраняет все текущие фильтры необработанных событий
- записывает N лучших отдельных событий в час
- группирует вывод по дням и часам
- записывает `raw.txt`, `raw.csv`, `raw.json`, `errors.log`

## Фильтры

Поддерживаемые фильтры применяются перед агрегацией или ранжированием:

- `--glob`
- `--filter key=value`
- `--duration`
- `--date-from YYYY-MM-DD`
- `--date-to YYYY-MM-DD`
- `--time-from HH:MM[:SS]`
- `--time-to HH:MM[:SS]`

## Пример

```bash
./techlog-stat.exe call-context --input C:/v8/logs --glob "rphost_*/*.log" --output C:/reports/call_2026-03-24 --top 10 --workers 10 --format text --filter Usr=DefUser --filter DataBase=conf_null --duration 1s
```

Топ событий за час:

```bash
./techlog-stat.exe call-context --mode raw --input C:/v8/logs --glob "rphost_*/*.log" --output C:/reports/call_raw_2026-03-24 --top 10 --filter Usr=DefUser --filter DataBase=conf_null --duration 5 --date-from 2026-03-24 --date-to 2026-03-24 --time-from 09:00 --time-to 18:00
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
