# techlog-stat

[![Build](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml/badge.svg)](https://github.com/azheval/tech_log/actions/workflows/build-techlog-stat.yml)

`techlog-stat` — аўтаномная CLI-ўтыліта на Go для аналізу тэхналагічнага журнала 1С і стварэння справаздач.

![img_001](/docs/img/001.png)

![img_002](/docs/img/002.png)

## Мовы

- English: [README.md](/README.md)
- Беларуская: `README.be.md`
- Русский: [README.ru.md](/README.ru.md)

## Дакументацыя

- Актуальная спецыфікацыя v2: [docs/techlog-stat-v2.be.md](/docs/techlog-stat-v2.be.md)
- English: [docs/techlog-stat-v2.en.md](/docs/techlog-stat-v2.en.md)
- Русский: [docs/techlog-stat-v2.ru.md](/docs/techlog-stat-v2.ru.md)
- Гістарычная спецыфікацыя v1: [docs/techlog-stat-v1.be.md](/docs/techlog-stat-v1.be.md)

## Каманды

Падтрымліваюцца ранейшыя спецыялізаваныя справаздачы:

- `sdbl-context`, `call-context`;
- `dbmssql-context`, `postgres-context`, `file-context`;
- `lock-context`, `timeout-context`, `deadlock-context`;
- `error-descr`.

Новыя каманды:

- `analyze` — адзіны аналіз усіх падзей за адзін праход;
- `compare` — параўнанне двух вынікаў `analyze` і пошук рэгрэсій.

## Лакальны web-інтэрфейс: `serve`

Запусціце лакальны інтэрфейс, задаўшы каталог, дзе дазволены пошук журналаў:

```powershell
./techlog-stat.exe serve --input C:/v8/logs --listen 127.0.0.1:8080
```

Адкрыйце паказаны лакальны адрас у браўзеры. Frontend убудаваны ў выканальны файл, аўтаномны і не выкарыстоўвае CDN або іншыя знешнія assets. У ім можна ствараць запускі аналізу, бачыць іх статус і progress, а таксама запытваць адмену. Форма дазваляе выбраць каталог толькі ў межах root з `--input` і glob файлаў адносна выбранага каталога; даступныя параметры аналізу і фільтры.

Укладкі «Агляд», «Падзеі», SQL, «Трасы», «Блакіроўкі» і «Зыходныя» дазваляюць фільтраваць радкі і пераходзіць ад захаваных агрэгатаў да зыходных падзей. Drill-down зыходных падзей паўторна чытае толькі файлы, matched канкрэтным запускам, і ніколі не прымае ад браўзера шлях або glob. У інтэрфейсе можна параўнаць два завершаныя in-memory запускі. Для выбранага запуску фарміруюцца downloads CSV, JSON і аўтаномнага HTML.

Сервер прымае толькі loopback-адрасы `--listen`; не выкарыстоўвайце яго як сеткавы сэрвіс. Запускі і вынікі захоўваюцца толькі ў памяці і знікаюць пры спыненні працэсу. Захоўванне абмежавана (`--max-runs`, па змаўчанні `8`): завершаныя запускі могуць выдаляцца, каб вызваліць месца; колькасць адначасовых аналізаў абмежавана `--max-concurrent` (па змаўчанні `1`).

## Адзіны аналіз

```powershell
./techlog-stat.exe analyze `
  --input C:/v8/logs `
  --glob "rphost_*/*.log" `
  --output C:/reports/overview `
  --workers 8 --bucket 1m --top 100 `
  --filter DataBase=conf_null --duration 500ms `
  --format text,csv,json,html
```

Фільтры `--filter`, `--duration`, `--date-from`, `--date-to`, `--time-from` і `--time-to` прымяняюцца да ўсіх раздзелаў: totals, SQL, трас, блакіровак.

Вынікі `analyze`:

- `summary.txt` — агульная зводка і раздзелы SQL, трас, SCALL, VRS, жыццевых цыклаў, працэсаў, ліцэнзій, кантэксту памылак і файлавай базы;
- `event_types.csv` — статыстыка па тыпах падзей;
- `sql.csv` — SQL/SDBL fingerprints і метрыкі;
- `traces.csv` — ланцужкі CALL, Context, SDBL, DBMS і памылак;
- `locks.csv` — блакіроўкі, канфлікты, рэгіены і сувязі;
- `scall.csv`, `web.csv`, `sessions.csv`, `processes.csv`, `licenses.csv`, `filedb.csv`, `error_contexts.csv` — дадатковыя раздзелы для расследавання;
- `run.json` — поўны машыначытальны вынік, у тым ліку новыя раздзелы і лічыльнікі якасці;
- `report.html` — аўтаномны інтэрактыўны dashboard з абмежаванымі панэлямі даступных раздзелаў;
- `errors.log` — памылкі асобных файлаў.

HTML працуе без CDN і падключэння да сеткі. Даступныя часавы графік, укладкі, пошук, сартаванне, фільтр `(unknown)` і дэталі падзей, якія раскрываюцца.

## Пашыраныя раздзелы analyze

`analyze` чытае выбраны паток журнала адзін раз і фарміруе асобныя раздзелы для:

- серверных выклікаў `SCALL` з групаваннем па інтэрфейсе, імені аб'екта, метадзе, кантэксце, карыстальніку, базе і працэсе;
- web-падзей `VRSREQUEST`, `VRSRESPONSE`, `VRSCACHE`: нармалізаваны URI, статус, байты, cache hit/miss і абмежаваныя павольныя/памылковыя прыклады;
- яўных жыццевых цыклаў `SESN`/`CONN`, актыўнасці `PROC`/`SCOM` і толькі яўна распазнаных сувязей працэсаў;
- ліцэнзій `LIC`/`HASP`, кантэксту памылак `EXCPCNTX` для сумяшчальных `EXCP`/`QERR` і пашыранай статыстыкі файлавай базы `DBV8DBEng`.

Карэляцыя наўмысна кансерватыўная: VRS-адказ або cache-падзея звязваецца толькі пры адзіным кандыдаце на сумяшчальным lane з узгодненымі ідэнтыфікатарамі; `EXCPCNTX` узбагачае толькі бліжэйшую сумяшчальную памылку. Неадназначныя запісы не звязваюцца па здагадцы і трапляюць у лічыльнікі якасці. Чакаючыя сувязі і прыклады для расследавання абмежаваны па памяці.

Новыя раздзелы даступныя ў `summary.txt`, асобных CSV (`scall.csv`, `web.csv`, `sessions.csv`, `processes.csv`, `licenses.csv`, `filedb.csv`, `error_contexts.csv`), `run.json` і адпаведных абмежаваных HTML-панэлях. Значэнні query у URI і ID-падобныя сегменты шляху нармалізуюцца; шляхі ліцэнзій, серыйныя значэнні, MAC-адрасы і абсалютныя шляхі файлавай базы рэдагуюцца ў адпаведных раздзелах.

Рэкамендацыі па зборы даных для расследавання тэхналагічных праблем: [афіцыйная старонка ІТС 1С](https://its.1c.ru/db/metod8dev/content/6005/hdoc).

## Параўнанне перыядаў

```powershell
./techlog-stat.exe compare `
  --baseline C:/reports/before/run.json `
  --current C:/reports/after/run.json `
  --output C:/reports/compare `
  --threshold-pct 5 --threshold-abs-us 1000 `
  --format text,csv,json,html
```

Каманда параўноўвае totals, тыпы падзей, карыстальнікаў, базы, працэсы і SQL fingerprints, а таксама SCALL `ByCall`, web requests, SESN/CONN `ByEvent`, `PROCByProcess`, `SCOMByOperation`, `LIC`, `DBV8DBEng` `ByFunc` і групы `EXCPCNTX`. Групы EXCPCNTX параўноўваюцца толькі па count і не ствараюць класіфікацыю рэгрэсіі прадукцыйнасці.

## Ранейшыя рэжымы

Aggregate-рэжым спецыялізаваных справаздач стварае `summary.txt`, CSV, `run.json` і `errors.log`. Рэжым `--mode raw` захоўвае top N асобных падзей кожнай гадзіны ў `raw.txt`, `raw.csv` і `raw.json`.

Падтрымліваюцца фарматы `text`, `csv`, `json`, `html`.

## Абмежаванні

- Імя файла журнала павінна пачынацца з `YYMMDDHH`.
- `--filter key=value` выкарыстоўвае дакладнае радковае супадзенне; паўторныя фільтры аб'ядноўваюцца праз AND.
- SQL fingerprinting замяняе літэралы, але не з'яўляецца паўнавартасным SQL parser.
- Карэляцыя трас пры адсутнасці ідэнтыфікатараў мае эўрыстычны характар.
- Утыліта аналізуе даступныя файлы пакетна і не з'яўляецца фонавай сістэмай маніторынгу.
