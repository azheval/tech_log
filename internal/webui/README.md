# Web UI REST contract

`webui.Handler()` serves this standalone client. The host owns all `/api/v1`
routes; requests and responses are JSON in UTF-8.

| Operation | Endpoint | Request / response minimum |
| --- | --- | --- |
| Read UI defaults | `GET /api/v1/config` | `{ "allowed_input_root": "...", "default_request": {input_root,glob,bucket_interval,top_n,workers,min_duration_micros} }`; fills the form, with `allowed_input_root` as empty-input fallback |
| List runs | `GET /api/v1/runs` | `{ "runs": [Run] }` |
| Start analysis | `POST /api/v1/runs` | `{input_root,glob,bucket_interval,top_n,workers,min_duration_micros,filters}` -> `Run` |
| Inspect / poll | `GET /api/v1/runs/{id}` | `Run`, with `status`, `progress` (0..1), `message` |
| Cancel | `POST /api/v1/runs/{id}/cancel` | `204` or updated `Run` |
| Delete | `POST /api/v1/runs/{id}/delete` | `204` or updated `Run` |
| Report section | `GET /api/v1/runs/{id}/report/overview?section=event_types|top_events|traces|sql_rows|users|databases|processes|locks&filter=&limit=&offset=` | `{ "items": [object], "columns": [string] }` |
| One trace | `GET /api/v1/runs/{id}/report/trace?id=` | `{ "items": [...] }` or a trace object |
| Source-event drill-down | `GET /api/v1/runs/{id}/report/source-events?event=&user=&database=&process=&contains=&limit=&offset=` | `{ "items": [...] }` |
| Compare | `POST /api/v1/report/compare` | `{baseline_run_id,current_run_id,threshold_percent?,threshold_abs_micros?}` -> comparison object or `Run` |
| Download | `GET /api/v1/runs/{id}/downloads/{filename}` | attachment; names include `run.json`, `report.html`, `event_types.csv`, `sql.csv` |

`Run` includes `id`, `status`, and may include `request.input_root`,
`created_at`, `progress`, `progress_percent`, `files_completed`,
`files_matched`, `message`, and `downloads`. The UI normalizes `progress`
objects as `current/total`, `progress_percent` (percent), and the ratio of
`files_completed/files_matched` to a progress value in 0..1. Terminal statuses
are `succeeded`, `failed`, and `canceled` (the older `completed` and
`cancelled` aliases are accepted); a running status is polled every two seconds.

The UI also accepts a bare array instead of `{items: [...]}` and object rows
with differing fields. Server values are rendered only through `textContent`;
no fetched value becomes markup. The HTML has no inline script, style, or event
handler, and works with the CSP supplied by `Handler`.
