# techlog-stat

[![Build](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml/badge.svg)](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml)

`techlog-stat` — аўтаномная CLI-ўтыліта на Go для чытання тэхналагічных журналаў 1С і запісу агрэгаваных справаздач у файлы.

## Мовы

- English: [README.md](/README.md)
- Беларуская: `README.be.md`
- Русский: [README.ru.md](/README.ru.md)

## Дакументацыя Версіі

- Падрабязная спецыфікацыя v1 па-беларуску: [docs/techlog-stat-v1.be.md](/docs/techlog-stat-v1.be.md)
- English version: [docs/techlog-stat-v1.en.md](/docs/techlog-stat-v1.en.md)
- Русская версия: [docs/techlog-stat-v1.ru.md](/docs/techlog-stat-v1.ru.md)

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

Агрэгаваны рэжым выкарыстоўваецца па змаўчанні:

- справаздачы аб кантэксце запісваюць `summary.txt`, `contexts.csv`, `run.json`, `errors.log`
- справаздачы аб памылках запісваюць `summary.txt`, `errors.csv`, `run.json`, `errors.log`

Рэжым raw уключаецца з дапамогай `--mode raw`:

- захоўвае ўсе бягучыя фільтры raw-падзей
- запісвае першыя N асобных падзей у гадзіну
- групуе вывад па дні і гадзіне
- запісвае `raw.txt`, `raw.csv`, `raw.json`, `errors.log`

## Фільтры

Падтрымліваемыя фільтры прымяняюцца перад агрэгацыяй або неапрацаваным ранжыраваннем:

- `--glob`
- `--filter key=value`
- `--duration`
- `--date-from YYYY-MM-DD`
- `--date-to YYYY-MM-DD`
- `--time-from HH:MM[:SS]`
- `--time-to HH:MM[:SS]`

## Прыклад

```bash
./techlog-stat.exe call-context --input C:/v8/logs --glob "rphost_*/*.log" --output C:/reports/call_2026-03-24 --top 10 --workers 10 --format text --filter Usr=DefUser --filter DataBase=conf_null --duration 1s
```

Топ падзеі за гадзіну:

```bash
./techlog-stat.exe call-context --mode raw --input C:/v8/logs --glob "rphost_*/*.log" --output C:/reports/call_raw_2026-03-24 --top 10 --filter Usr=DefUser --filter DataBase=conf_null --duration 5 --date-from 2026-03-24 --date-to 2026-03-24 --time-from 09:00 --time-to 18:00
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
