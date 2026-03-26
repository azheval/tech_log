# techlog-stat

[![Build](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml/badge.svg)](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml)

`techlog-stat` — аўтаномная CLI-ўтыліта на Go для чытання тэхналагічных журналаў 1С і запісу агрэгаваных справаздач у файлы.

## Мовы

- English: [README.md](c:/ws/tech_log/go/techlog-stat/README.md)
- Беларуская: `README.be.md`
- Русский: [README.ru.md](c:/ws/tech_log/go/techlog-stat/README.ru.md)

## Дакументацыя Версіі

- Падрабязная спецыфікацыя v1 па-беларуску: [docs/techlog-stat-v1.be.md](c:/ws/tech_log/go/techlog-stat/docs/techlog-stat-v1.be.md)
- English version: [docs/techlog-stat-v1.en.md](c:/ws/tech_log/go/techlog-stat/docs/techlog-stat-v1.en.md)
- Русская версия: [docs/techlog-stat-v1.ru.md](c:/ws/tech_log/go/techlog-stat/docs/techlog-stat-v1.ru.md)

## Бягучыя Справаздачы

Падтрымліваюцца справаздачы:

- `sdbl-context`
- `call-context`
- `dbmssql-context`
- `postgres-context` або `dbpostgrs-context`
- `file-context` або `dbv8dbeng-context`
- `lock-context` або `locks-context`
- `timeout-context`
- `deadlock-context`
- `error-descr` або `excp-descr`

Кантэкстныя справаздачы:

- чытаюць `*.log` наўпрост
- агрэгуюць працягласць і колькасць па `Context`
- запісваюць `summary.txt`, `contexts.csv`, `run.json`, `errors.log`

Справаздачы па памылках:

- чытаюць `*.log` наўпрост
- агрэгуюць `EXCP` і `QERR` па нармалізаваным `Descr`
- запісваюць `summary.txt`, `errors.csv`, `run.json`, `errors.log`

## Прыклад

```bash
./techlog-stat.exe call-context --input C:/v8/logs --glob "rphost_*/*.log" --output C:/reports/call_2026-03-24 --top 10 --workers 10 --format text --filter Usr=DefUser --filter DataBase=conf_null --duration 1s
```

## Нататкі

Падзеі базы даных супастаўляюцца так:

- `dbmssql-context` -> `DBMSSQL`
- `postgres-context` / `dbpostgrs-context` -> `DBPOSTGRS`
- `file-context` / `dbv8dbeng-context` -> `DBV8DBEng`

Падзеі блакіровак супастаўляюцца так:

- `lock-context` / `locks-context` -> `TLOCK`, `TTIMEOUT`, `TDEADLOCK`
- `timeout-context` -> `TTIMEOUT`
- `deadlock-context` -> `TDEADLOCK`

Падзеі памылак супастаўляюцца так:

- `error-descr` / `excp-descr` -> `EXCP`, `QERR`
- апісанні нармалізуюцца па аналогіі з legacy Perl-скрыптам:
  - IPv6 endpoints -> `{IPV6}`
  - IPv4 endpoints -> `{IPV4}`
  - UUIDs -> `{UUID}`
  - фрагменты віду `пачат: dd.mm.yyyy у hh:mm:ss` -> `{DtTm}`
