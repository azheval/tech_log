# techlog-stat

[![Build](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml/badge.svg)](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml)

`techlog-stat` — автономная CLI-утилита на Go для анализа технологического журнала 1С и формирования отчетов.

![img_001](/docs/img/001.png)

![img_002](/docs/img/002.png)

## Языки

- English: [README.md](/README.md)
- Русский: `README.ru.md`
- Беларуская: [README.be.md](/README.be.md)

## Документация

- Актуальная спецификация v2: [docs/techlog-stat-v2.ru.md](/docs/techlog-stat-v2.ru.md)
- English: [docs/techlog-stat-v2.en.md](/docs/techlog-stat-v2.en.md)
- Беларуская: [docs/techlog-stat-v2.be.md](/docs/techlog-stat-v2.be.md)
- Историческая спецификация v1: [docs/techlog-stat-v1.ru.md](/docs/techlog-stat-v1.ru.md)

## Команды

Поддерживаются прежние специализированные отчеты:

- `sdbl-context`, `call-context`;
- `dbmssql-context`, `postgres-context`, `file-context`;
- `lock-context`, `timeout-context`, `deadlock-context`;
- `error-descr`.

Новые команды:

- `analyze` — единый анализ всех событий за один проход;
- `compare` — сравнение двух результатов `analyze` и поиск регрессий.

## Локальный web-интерфейс: `serve`

Запустите локальный интерфейс, указав каталог, в котором разрешено искать журналы:

```powershell
./techlog-stat.exe serve --input C:/v8/logs --listen 127.0.0.1:8080
```

Откройте показанный локальный адрес в браузере. Frontend встроен в исполняемый файл, автономен и не использует CDN или другие внешние assets. В нем создают запуски анализа, наблюдают статус и progress, а также запрашивают отмену. В форме можно выбрать каталог только внутри root из `--input` и glob файлов относительно выбранного каталога; доступны параметры анализа и фильтры.

Вкладки «Обзор», «События», SQL, «Трассы», «Блокировки» и «Исходные» позволяют фильтровать строки и переходить от сохраненных агрегатов к исходным событиям. Drill-down исходных событий перечитывает только файлы, matched конкретным запуском, и никогда не принимает от браузера путь или glob. В интерфейсе можно сравнить два завершенных in-memory запуска. Для выбранного запуска формируются downloads CSV, JSON и автономного HTML.

Сервер принимает только loopback-адреса `--listen`; не используйте его как сетевой сервис. Запуски и результаты хранятся только в памяти и теряются при остановке процесса. Хранение ограничено (`--max-runs`, по умолчанию `8`): завершенные запуски могут удаляться, чтобы освободить место; число одновременных анализов ограничено `--max-concurrent` (по умолчанию `1`).

## Единый анализ

```powershell
./techlog-stat.exe analyze `
  --input C:/v8/logs `
  --glob "rphost_*/*.log" `
  --output C:/reports/overview `
  --workers 8 --bucket 1m --top 100 `
  --filter DataBase=conf_null --duration 500ms `
  --format text,csv,json,html
```

Фильтры `--filter`, `--duration`, `--date-from`, `--date-to`, `--time-from` и `--time-to` применяются ко всем разделам: totals, SQL, трассам и блокировкам.

Результаты `analyze`:

- `summary.txt` — общая сводка и разделы SQL, трасс, SCALL, VRS, жизненных циклов, процессов, лицензий, контекста ошибок и файловой базы;
- `event_types.csv` — статистика по типам событий;
- `sql.csv` — SQL/SDBL fingerprints и метрики;
- `traces.csv` — цепочки CALL, Context, SDBL, DBMS и ошибок;
- `locks.csv` — блокировки, конфликты, регионы и связи;
- `scall.csv`, `web.csv`, `sessions.csv`, `processes.csv`, `licenses.csv`, `filedb.csv`, `error_contexts.csv` — дополнительные разделы для расследования;
- `run.json` — полный машиночитаемый результат, включая новые разделы и счетчики качества;
- `report.html` — автономный интерактивный dashboard с ограниченными панелями доступных разделов;
- `errors.log` — ошибки отдельных файлов.

HTML работает без CDN и подключения к сети. В нем есть временной график, вкладки, поиск, сортировка, фильтр `(unknown)` и раскрываемые детали событий.

## Расширенные разделы analyze

`analyze` читает выбранный поток журнала один раз и формирует отдельные разделы для:

- серверных вызовов `SCALL` с группировкой по интерфейсу, имени объекта, методу, контексту, пользователю, базе и процессу;
- web-событий `VRSREQUEST`, `VRSRESPONSE`, `VRSCACHE`: нормализованный URI, статус, байты, cache hit/miss и ограниченные медленные/ошибочные примеры;
- явных жизненных циклов `SESN`/`CONN`, активности `PROC`/`SCOM` и только явно распознанных связей процессов;
- лицензий `LIC`/`HASP`, контекста ошибок `EXCPCNTX` для совместимых `EXCP`/`QERR` и расширенной статистики файловой базы `DBV8DBEng`.

Корреляция намеренно консервативна: VRS-ответ или cache-событие связывается только при единственном кандидате на совместимом lane с согласованными идентификаторами; `EXCPCNTX` обогащает только ближайшую совместимую ошибку. Неоднозначные записи не связываются по догадке и отражаются в счетчиках качества. Ожидающие связи и примеры для расследования ограничены по памяти.

Новые разделы доступны в `summary.txt`, отдельных CSV (`scall.csv`, `web.csv`, `sessions.csv`, `processes.csv`, `licenses.csv`, `filedb.csv`, `error_contexts.csv`), `run.json` и соответствующих ограниченных HTML-панелях. Значения query в URI и ID-подобные сегменты пути нормализуются; пути лицензий, серийные значения, MAC-адреса и абсолютные пути файловой базы редактируются в соответствующих разделах.

Рекомендации по сбору данных для расследования технологических проблем: [официальная страница ИТС 1С](https://its.1c.ru/db/metod8dev/content/6005/hdoc).

## Сравнение периодов

```powershell
./techlog-stat.exe compare `
  --baseline C:/reports/before/run.json `
  --current C:/reports/after/run.json `
  --output C:/reports/compare `
  --threshold-pct 5 --threshold-abs-us 1000 `
  --format text,csv,json,html
```

Команда сравнивает totals, типы событий, пользователей, базы, процессы и SQL fingerprints, а также SCALL `ByCall`, web requests, SESN/CONN `ByEvent`, `PROCByProcess`, `SCOMByOperation`, `LIC`, `DBV8DBEng` `ByFunc` и группы `EXCPCNTX`. Группы EXCPCNTX сравниваются только по count и не создают классификацию регрессии производительности.

## Прежние режимы

Aggregate-режим специализированных отчетов создает `summary.txt`, CSV, `run.json` и `errors.log`. Режим `--mode raw` сохраняет top N отдельных событий каждого часа в `raw.txt`, `raw.csv` и `raw.json`.

Поддерживаемые форматы: `text`, `csv`, `json`, `html`.

## Ограничения

- Имя файла журнала должно начинаться с `YYMMDDHH`.
- `--filter key=value` использует точное строковое совпадение; повторные фильтры объединяются через AND.
- SQL fingerprinting заменяет литералы, но не является полноценным SQL parser.
- Корреляция трасс при отсутствии идентификаторов носит эвристический характер.
- Утилита анализирует доступные файлы пакетно и не является фоновой системой мониторинга.
