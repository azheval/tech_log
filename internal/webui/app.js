(() => {
  "use strict";

  const api = "/api/v1";
  const columnSettingsKey = "techlog-stat:column-settings:v1";
  const reportViews = new Set(["overview", "events", "sql", "traces", "locks", "sources"]);
  const groupingSections = new Set(["event_types", "time_buckets", "contexts", "users", "databases", "processes"]);
  const state = { runs: [], selected: null, view: "overview", rows: [], tableRows: [], columnCatalog: [], columnSettings: {}, poll: 0, searchTimer: 0 };
  const $ = (id) => document.getElementById(id);
  const el = (name, text, className) => {
    const node = document.createElement(name);
    if (text !== undefined) node.textContent = String(text);
    if (className) node.className = className;
    return node;
  };

  function uniqueColumns(columns) {
    const found = new Set();
    return (Array.isArray(columns) ? columns : []).filter((column) => {
      if (typeof column !== "string" || !column || found.has(column)) return false;
      found.add(column);
      return true;
    });
  }

  function loadColumnSettings() {
    try {
      const raw = window.localStorage.getItem(columnSettingsKey);
      if (!raw) return {};
      const parsed = JSON.parse(raw);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};
      const settings = {};
      Object.entries(parsed).forEach(([view, value]) => {
        const groupedView = view.startsWith("overview:") && groupingSections.has(view.slice("overview:".length));
        if ((!reportViews.has(view) && !groupedView) || !value || typeof value !== "object" || Array.isArray(value)) return;
        const order = uniqueColumns(value.order);
        const hidden = uniqueColumns(value.hidden);
        if (order.length || hidden.length) settings[view] = { order, hidden };
      });
      return settings;
    } catch (_) { return {}; }
  }

  function saveColumnSettings() {
    try { window.localStorage.setItem(columnSettingsKey, JSON.stringify(state.columnSettings)); } catch (_) { /* Storage can be unavailable. */ }
  }

  function currentColumnState(columns) {
    const defaults = uniqueColumns(columns);
    const available = new Set(defaults);
    const saved = state.columnSettings[columnSettingsScope()] || {};
    const order = uniqueColumns(saved.order).filter((column) => available.has(column));
    defaults.forEach((column) => { if (!order.includes(column)) order.push(column); });
    const hidden = new Set(uniqueColumns(saved.hidden).filter((column) => available.has(column)));
    return { defaults, order, hidden };
  }

  function storeColumnState(order, hidden) {
    state.columnSettings[columnSettingsScope()] = { order: uniqueColumns(order), hidden: Array.from(hidden) };
    saveColumnSettings();
  }

  function columnSettingsScope() {
    return state.view === "overview" ? "overview:" + $("group-by").value : state.view;
  }

  function closeColumnPanel() {
    const button = $("column-settings");
    $("column-panel").hidden = true;
    button.setAttribute("aria-expanded", "false");
  }

  state.columnSettings = loadColumnSettings();

  async function request(path, options) {
    const response = await fetch(api + path, Object.assign({ headers: { Accept: "application/json" } }, options));
    if (!response.ok) {
      let detail = response.statusText;
      try {
        const body = await response.json();
        detail = body.message || (body.error && (body.error.message || body.error.code)) || detail;
      } catch (_) { /* response can be empty */ }
      throw new Error(detail || "API error");
    }
    if (response.status === 204) return null;
    const type = response.headers.get("content-type") || "";
    return type.includes("application/json") ? response.json() : null;
  }

  function items(payload) {
    if (Array.isArray(payload)) return payload;
    if (payload && Array.isArray(payload.items)) return payload.items;
    if (payload && Array.isArray(payload.runs)) return payload.runs;
    if (payload && Array.isArray(payload.rows)) return payload.rows;
    return [];
  }

  function endpointRun(id) { return "/runs/" + encodeURIComponent(id); }
  function normalizeRun(raw) {
    const run = Object.assign({}, raw || {});
    if (run.progress && typeof run.progress === "object") run.progress_detail = Object.assign({}, run.progress);
    let progress = Number(run.progress);
    if (!Number.isFinite(progress)) {
      const percent = Number(run.progress_percent);
      if (Number.isFinite(percent)) progress = percent / 100;
      else {
        const completed = Number(run.files_completed); const matched = Number(run.files_matched);
        if (Number.isFinite(completed) && Number.isFinite(matched) && matched > 0) progress = completed / matched;
        else if (run.progress && typeof run.progress === "object") {
          const current = Number(run.progress.current); const total = Number(run.progress.total);
          if (Number.isFinite(current) && Number.isFinite(total) && total > 0) progress = current / total;
        }
      }
    }
    if (Number.isFinite(progress)) run.progress = Math.min(1, Math.max(0, progress));
    return run;
  }
  function terminal(run) { return ["succeeded", "failed", "canceled", "completed", "cancelled"].includes(String(run.status || "").toLowerCase()); }
  function message(text, error) { const target = $("run-message"); target.textContent = text || ""; target.classList.toggle("error", Boolean(error)); }
  function status(text, error) {
    const target = $("api-status");
    const dot = el("span");
    dot.setAttribute("aria-hidden", "true");
    target.replaceChildren(dot, document.createTextNode(text));
    target.classList.toggle("error", Boolean(error));
    target.classList.remove("pending");
  }

  const sidebarStorageKey = "techlog-stat.sidebar-collapsed";
  function setSidebarCollapsed(collapsed, save) {
    const button = $("toggle-sidebar");
    const sidebar = $("control-sidebar");
    document.body.classList.toggle("sidebar-collapsed", collapsed);
    button.setAttribute("aria-expanded", String(!collapsed));
    button.title = collapsed ? "Show control panel" : "Hide control panel";
    button.querySelector(".sidebar-toggle-label").textContent = collapsed ? "Show panel" : "Hide panel";
    sidebar.setAttribute("aria-hidden", String(collapsed));
    if (save) {
      try { window.localStorage.setItem(sidebarStorageKey, String(collapsed)); } catch (_) { /* Storage can be unavailable. */ }
    }
  }

  function restoreSidebar() {
    let collapsed = false;
    try { collapsed = window.localStorage.getItem(sidebarStorageKey) === "true"; } catch (_) { /* Storage can be unavailable. */ }
    setSidebarCollapsed(collapsed, false);
  }

  function bucketControlValue(value) {
    const text = String(value);
    return { "1m0s": "1m", "5m0s": "5m", "15m0s": "15m", "1h0m0s": "1h" }[text] || text;
  }

  async function loadConfig() {
    try {
      const config = await request("/config");
      const defaults = (config && config.default_request) || {};
      const input = $("input");
      if (!input.value && (defaults.input_root || (config && config.allowed_input_root))) input.value = String(defaults.input_root || config.allowed_input_root);
      const fields = { glob: "glob", bucket_interval: "bucket", top_n: "top", workers: "workers", min_duration_micros: "min-duration" };
      Object.entries(fields).forEach(([property, id]) => {
        if (defaults[property] !== undefined && defaults[property] !== null) $(id).value = id === "bucket" ? bucketControlValue(defaults[property]) : String(defaults[property]);
      });
    } catch (_) { /* The runs endpoint reports API availability separately. */ }
  }

  function runLabel(run) {
    const bits = [(run.request && run.request.input_root) || run.input_root || run.input || run.name || run.id];
    if (run.created_at) bits.push(run.created_at);
    return bits.filter(Boolean).join(" · ");
  }

  function statusLabel(value) {
    return {
      queued: "queued", running: "running", succeeded: "complete", completed: "complete",
      canceled: "canceled", cancelled: "canceled", failed: "failed"
    }[String(value || "").toLowerCase()] || String(value || "unknown");
  }

  function progressLabel(run) {
    if (!Number.isFinite(Number(run.progress))) return "";
    const percent = Math.round(Number(run.progress) * 100);
    const detail = run.progress_detail || {};
    const completed = Number(run.files_completed !== undefined ? run.files_completed : detail.current);
    const matched = Number(run.files_matched !== undefined ? run.files_matched : detail.total);
    if (Number.isFinite(completed) && Number.isFinite(matched) && matched > 0) return percent + "% · " + completed + " of " + matched + " files";
    return percent + "%";
  }

  function renderRuns() {
    const root = $("runs");
    root.replaceChildren();
    if (!state.runs.length) { root.append(el("p", "No runs yet.", "empty")); return; }
    state.runs.forEach((run) => {
      const button = el("button", undefined, "run");
      button.type = "button";
      button.classList.toggle("selected", state.selected && run.id === state.selected.id);
      button.setAttribute("aria-pressed", String(Boolean(state.selected && run.id === state.selected.id)));
      const name = el("span", runLabel(run), "run-name");
      name.title = runLabel(run);
      const right = el("span");
      const badge = el("span", statusLabel(run.status), "badge " + String(run.status || "unknown").toLowerCase());
      right.append(badge);
      if (!terminal(run) && Number.isFinite(Number(run.progress))) {
        const progress = document.createElement("progress");
        progress.className = "progress"; progress.max = 1; progress.value = Number(run.progress); progress.setAttribute("aria-label", "Progress: " + progressLabel(run)); progress.title = progressLabel(run);
        right.append(progress, el("span", progressLabel(run), "progress-label"));
      }
      button.append(name, right);
      button.addEventListener("click", () => selectRun(run));
      root.append(button);
    });
    populateCompare();
  }

  function populateCompare() {
    [$("baseline"), $("current")].forEach((select) => {
      const previous = select.value;
      select.replaceChildren(el("option", "Select a run"));
      select.firstChild.value = "";
      state.runs.filter((run) => run.status === "succeeded" || run.status === "completed").forEach((run) => {
        const option = el("option", runLabel(run)); option.value = run.id; select.append(option);
      });
      select.value = previous;
    });
  }

  function renderDownloads(run) {
    const root = $("downloads"); root.replaceChildren();
    if (!run || !run.id) return;
    [["CSV", "event_types.csv"], ["JSON", "run.json"], ["HTML", "report.html"]].forEach(([label, filename]) => {
      const link = el("a", label); link.href = api + endpointRun(run.id) + "/downloads/" + filename; link.download = "";
      root.append(link);
    });
  }

  async function refreshRuns() {
    try {
	  const payload = await request("/runs");
	  state.runs = items(payload).map(normalizeRun);
	  const updated = state.selected && state.runs.find((run) => run.id === state.selected.id);
	  const completedNow = updated && !terminal(state.selected) && terminal(updated) && (updated.status === "succeeded" || updated.status === "completed");
	  if (updated) state.selected = Object.assign({}, state.selected, updated);
	  renderRuns(); status("API: connected");
	  $("cancel-run").disabled = !state.selected || terminal(state.selected);
	  if (completedNow) await loadSection();
    } catch (error) { status("API: unavailable", true); message("Could not retrieve the run list: " + error.message, true); }
  }

  async function selectRun(run) {
    state.selected = run; state.rows = []; $("selected-run").textContent = "RUN " + run.id;
    $("cancel-run").disabled = terminal(run); renderRuns(); renderDownloads(run); clearDrilldown();
    try {
      const detail = await request(endpointRun(run.id));
      if (detail) state.selected = normalizeRun(detail.run || detail);
      $("selected-run").textContent = "RUN " + state.selected.id + " · " + (state.selected.status || "");
      $("cancel-run").disabled = terminal(state.selected); renderRuns(); renderDownloads(state.selected);
      await loadSection();
    } catch (error) { message("Could not open the run: " + error.message, true); }
  }

  function filters() {
    return { q: $("search").value.trim(), severity: $("severity").value, limit: Number($("limit").value) || 100 };
  }

  function queryString(filter) {
    const query = new URLSearchParams();
    if (filter.q) query.set("filter", filter.q);
    if (filter.severity) query.set("severity", filter.severity);
    query.set("limit", String(filter.limit));
    query.set("offset", "0");
    return "?" + query.toString();
  }

  function valueText(value) {
    if (value === null || value === undefined) return "";
    if (typeof value === "object") { try { return JSON.stringify(value); } catch (_) { return "[data]"; } }
    return String(value);
  }

  function localFilter(rows, filter) {
    const needle = filter.q.toLocaleLowerCase("en");
    return rows.filter((row) => {
      const text = valueText(row).toLocaleLowerCase("en");
      const severity = String(row.severity || row.level || "").toLowerCase();
      return (!needle || text.includes(needle)) && (!filter.severity || severity === filter.severity);
    }).slice(0, filter.limit);
  }

  function columnsFor(rows, specified) {
    if (Array.isArray(specified) && specified.length) return specified;
    const found = new Set(); rows.slice(0, 100).forEach((row) => Object.keys(row || {}).forEach((key) => found.add(key)));
    return Array.from(found);
  }

  function aggregateKey(row) {
    if (state.view === "sql") return row.sample || row.Sample || row.normalized_query || row.NormalizedQuery || row.fingerprint || row.Fingerprint;
    return row.key || row.Key || row.value || row.Value || row.name || row.Name || row.event || row.Event || row.event_type || row.EventType || row.fingerprint || row.Fingerprint || row.sql;
  }

  function canDrillAggregate() {
    return state.view !== "sources" && !(state.view === "overview" && $("group-by").value === "time_buckets");
  }
  function numberColumn(key) { return /count|duration|time|avg|p\d+|percent|size|total/i.test(key); }

  function refreshColumnView() {
    renderRows(state.tableRows, state.columnCatalog);
  }

  function renderColumnPanel() {
    const button = $("column-settings");
    const panel = $("column-panel");
    const settings = currentColumnState(state.columnCatalog);
    button.disabled = !settings.defaults.length;
    if (!settings.defaults.length) {
      closeColumnPanel();
      panel.replaceChildren();
      return;
    }
    panel.replaceChildren();
    panel.append(el("p", "Select visible columns and their order.", "column-panel-description"));
    const list = el("ul", undefined, "column-list");
    settings.order.forEach((column, index) => {
      const item = el("li", undefined, "column-item");
      const label = el("label", undefined, "column-choice");
      const input = document.createElement("input");
      input.type = "checkbox";
      input.checked = !settings.hidden.has(column);
      input.setAttribute("aria-label", "Show column " + column);
      input.addEventListener("change", () => {
        if (input.checked) settings.hidden.delete(column);
        else settings.hidden.add(column);
        storeColumnState(settings.order, settings.hidden);
        refreshColumnView();
      });
      label.append(input, el("span", column));
      const actions = el("div", undefined, "column-actions");
      const up = el("button", "↑", "column-move");
      up.type = "button";
      up.disabled = index === 0;
      up.setAttribute("aria-label", "Move column " + column + " up");
      up.addEventListener("click", () => {
        const order = settings.order.slice();
        [order[index - 1], order[index]] = [order[index], order[index - 1]];
        storeColumnState(order, settings.hidden);
        refreshColumnView();
      });
      const down = el("button", "↓", "column-move");
      down.type = "button";
      down.disabled = index === settings.order.length - 1;
      down.setAttribute("aria-label", "Move column " + column + " down");
      down.addEventListener("click", () => {
        const order = settings.order.slice();
        [order[index], order[index + 1]] = [order[index + 1], order[index]];
        storeColumnState(order, settings.hidden);
        refreshColumnView();
      });
      actions.append(up, down);
      item.append(label, actions);
      list.append(item);
    });
    const reset = el("button", "Restore default order", "secondary column-reset");
    reset.type = "button";
    reset.addEventListener("click", () => {
      storeColumnState(settings.defaults, new Set());
      refreshColumnView();
    });
    panel.append(list, reset);
  }

  function renderRows(rows, specified) {
    const root = $("view"); root.replaceChildren();
    const defaults = columnsFor(rows, specified);
    state.tableRows = Array.isArray(rows) ? rows.slice() : [];
    state.columnCatalog = uniqueColumns(defaults);
    renderColumnPanel();
    if (!rows.length) { root.append(el("p", "No data for the selected filter.", "empty")); return; }
    const settings = currentColumnState(defaults);
    const columns = settings.order.filter((column) => !settings.hidden.has(column));
    if (!columns.length) { root.append(el("p", "No columns selected. Open column settings to show data.", "empty")); return; }
    const wrapper = el("div", undefined, "table-wrap"); const table = el("table", undefined, "data-table");
    const head = document.createElement("thead"); const headRow = document.createElement("tr");
    columns.forEach((column) => { const cell = el("th", column); if (numberColumn(column)) cell.className = "numeric"; cell.scope = "col"; headRow.append(cell); });
    head.append(headRow); table.append(head); const body = document.createElement("tbody");
    rows.forEach((row) => {
      const tr = document.createElement("tr"); const key = aggregateKey(row);
      columns.forEach((column) => {
        const td = document.createElement("td"); if (numberColumn(column)) td.className = "numeric";
        const value = valueText(row[column]);
        const traceID = state.view === "traces" && (row.trace_id || row.id || row.ID);
        if (traceID && (column === "trace_id" || column === "id" || column === "ID")) {
          const drill = el("button", value, "drill-button"); drill.type = "button"; drill.addEventListener("click", () => loadTrace(String(traceID))); td.append(drill);
        } else if (key && value === String(key) && canDrillAggregate()) {
          const drill = el("button", value, "drill-button"); drill.type = "button"; drill.addEventListener("click", () => openDrilldown(String(key))); td.append(drill);
        } else td.textContent = value;
        tr.append(td);
      }); body.append(tr);
    }); table.append(body); wrapper.append(table); root.append(wrapper);
  }

  function reportSection() {
    return { overview: $("group-by").value, events: "top_events", sql: "sql_rows", traces: "traces", locks: "locks" }[state.view];
  }

  function sourceEventsQuery(filter, key) {
    const query = new URLSearchParams({ limit: String(filter.limit), offset: "0" });
    const text = key || filter.q;
    if (text) {
      if (state.view === "overview") {
        const parameter = { event_types: "event", users: "user", databases: "database", processes: "process", contexts: "context" }[$("group-by").value] || "contains";
        query.set(parameter, text);
      } else if (state.view === "events" || state.view === "locks") query.set("event", text);
      else if (state.view === "traces") query.set("process", text);
      else query.set("contains", text);
    }
    return query.toString();
  }

  async function loadSection() {
    if (!state.selected) return;
    const groupSelect = $("group-by");
    groupSelect.disabled = state.view !== "overview";
    const title = state.view === "overview" ? "Grouping: " + groupSelect.options[groupSelect.selectedIndex].textContent : document.querySelector('[data-view="' + state.view + '"]').textContent;
    $("view-title").textContent = title; $("view").setAttribute("aria-labelledby", "tab-" + state.view);
    $("view").replaceChildren(el("p", "Loading…", "empty"));
    const filter = filters();
    try {
      let payload;
      if (state.view === "sources") {
        payload = await request(endpointRun(state.selected.id) + "/report/source-events?" + sourceEventsQuery(filter));
      } else {
        const query = new URLSearchParams(queryString(filter).slice(1));
        query.set("section", reportSection());
        payload = await request(endpointRun(state.selected.id) + "/report/overview?" + query.toString());
      }
      state.rows = items(payload); renderRows(localFilter(state.rows, filter), payload && payload.columns);
    } catch (error) { $("view").replaceChildren(el("p", "Could not load section: " + error.message, "empty")); }
  }

  function clearDrilldown() { const box = $("drilldown"); box.hidden = true; box.replaceChildren(); }
  async function loadTrace(id) {
    if (!state.selected) return;
    $("view").replaceChildren(el("p", "Loading trace…", "empty"));
    try {
      const payload = await request(endpointRun(state.selected.id) + "/report/trace?id=" + encodeURIComponent(id));
      const rows = items(payload); renderRows(rows.length ? rows : [payload], payload && payload.columns);
      $("view-title").textContent = "Trace " + id; $("view").focus();
    } catch (error) { $("view").replaceChildren(el("p", "Could not load trace: " + error.message, "empty")); }
  }
  function openDrilldown(key) {
    const box = $("drilldown"); box.hidden = false; box.replaceChildren();
    const close = el("button", "Close"); close.type = "button"; close.addEventListener("click", clearDrilldown);
    const label = el("strong", "Aggregate details: " + key); const samples = el("button", "Source events"); const events = el("button", "Top events");
    samples.type = "button"; events.type = "button";
    samples.addEventListener("click", () => loadDrilldown(key, "samples")); events.addEventListener("click", () => loadDrilldown(key, "top-events"));
    box.append(close, label, document.createTextNode(" "), samples, document.createTextNode(" "), events);
  }

  async function loadDrilldown(key, kind) {
    if (!state.selected) return;
    const box = $("drilldown"); box.append(el("p", "Loading " + kind + "…"));
    try {
      let payload;
      if (kind === "samples") {
        payload = await request(endpointRun(state.selected.id) + "/report/source-events?" + sourceEventsQuery(filters(), key));
      } else {
        const query = new URLSearchParams({ section: "top_events", filter: key, limit: String(filters().limit), offset: "0" });
        payload = await request(endpointRun(state.selected.id) + "/report/overview?" + query.toString());
      }
      const rows = items(payload);
      $("view-title").textContent = kind === "samples" ? "Source events" : "Top events";
      renderRows(rows, payload && payload.columns); $("view").focus();
    } catch (error) { box.append(el("p", "Could not retrieve details: " + error.message, "message error")); }
  }

  async function startRun(event) {
    event.preventDefault();
    const body = {
      input_root: $("input").value.trim(), glob: $("glob").value.trim(), bucket_interval: $("bucket").value,
      top_n: Number($("top").value), workers: Number($("workers").value), min_duration_micros: Number($("min-duration").value), filters: []
    };
    message("Run submitted…");
    try {
      const run = await request("/runs", { method: "POST", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify(body) });
      message("Run created."); await refreshRuns(); if (run && run.id) await selectRun(run);
    } catch (error) { message("Could not create run: " + error.message, true); }
  }

  async function cancelRun() {
    if (!state.selected) return;
    try { await request(endpointRun(state.selected.id) + "/cancel", { method: "POST" }); message("Cancellation requested."); await refreshRuns(); }
    catch (error) { message("Could not cancel run: " + error.message, true); }
  }

  async function compareRuns() {
    const baseline = $("baseline").value; const current = $("current").value;
    if (!baseline || !current) { message("Select baseline and current runs.", true); return; }
    if (baseline === current) { message("Comparison requires different runs.", true); return; }
    try {
      const result = await request("/report/compare", { method: "POST", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify({ baseline_run_id: baseline, current_run_id: current }) });
      message("Comparison complete.");
      if (result && result.id) { await refreshRuns(); await selectRun(result); }
      else { state.rows = comparisonRows(result); $("view-title").textContent = "Comparison"; renderRows(state.rows); }
    } catch (error) { message("Could not compare runs: " + error.message, true); }
  }

  function comparisonRows(result) {
    if (!result || typeof result !== "object") return [];
    const rows = [];
    Object.entries(result).forEach(([section, value]) => {
      if (Array.isArray(value)) value.forEach((row) => rows.push(Object.assign({ section }, row && typeof row === "object" ? row : { value: row })));
      else if (value && typeof value === "object" && section === "totals") rows.push(Object.assign({ section }, value));
    });
    return rows;
  }

  function activateTab(tab) {
    state.view = tab.dataset.view; clearDrilldown(); closeColumnPanel();
    $("group-by").disabled = state.view !== "overview";
    document.querySelectorAll("[role=tab]").forEach((item) => {
      const selected = item === tab;
      item.setAttribute("aria-selected", String(selected));
      item.tabIndex = selected ? 0 : -1;
    });
    loadSection();
  }

  function chooseTab(event) { activateTab(event.currentTarget); }

  function navigateTabs(event) {
    const tabs = Array.from(document.querySelectorAll("[role=tab]"));
    const current = tabs.indexOf(event.currentTarget);
    let next = current;
    if (event.key === "ArrowRight") next = (current + 1) % tabs.length;
    else if (event.key === "ArrowLeft") next = (current - 1 + tabs.length) % tabs.length;
    else if (event.key === "Home") next = 0;
    else if (event.key === "End") next = tabs.length - 1;
    else return;
    event.preventDefault();
    tabs[next].focus();
    activateTab(tabs[next]);
  }

  restoreSidebar();
  $("toggle-sidebar").addEventListener("click", () => {
    setSidebarCollapsed(!document.body.classList.contains("sidebar-collapsed"), true);
  });
  $("run-form").addEventListener("submit", startRun); $("cancel-run").addEventListener("click", cancelRun); $("compare").addEventListener("click", compareRuns); $("refresh-runs").addEventListener("click", refreshRuns);
  $("column-settings").addEventListener("click", () => {
    const panel = $("column-panel");
    const open = panel.hidden;
    panel.hidden = !open;
    $("column-settings").setAttribute("aria-expanded", String(open));
    if (open) renderColumnPanel();
  });
  document.querySelectorAll("[role=tab]").forEach((tab) => {
    tab.addEventListener("click", chooseTab);
    tab.addEventListener("keydown", navigateTabs);
  });
  $("search").addEventListener("input", () => {
    window.clearTimeout(state.searchTimer);
    state.searchTimer = window.setTimeout(() => { if (state.selected) loadSection(); }, 280);
  });
  $("group-by").addEventListener("change", () => {
    closeColumnPanel();
    if (state.selected && state.view === "overview") loadSection();
  });
  $("reset-filters").addEventListener("click", () => {
    $("search").value = "";
    $("group-by").value = "event_types";
    $("severity").value = "";
    $("limit").value = "100";
    window.clearTimeout(state.searchTimer);
    if (state.selected) loadSection();
  });
  [$("severity"), $("limit")].forEach((field) => field.addEventListener("change", () => { if (state.selected) loadSection(); }));
  loadConfig(); refreshRuns(); state.poll = window.setInterval(() => { if (state.runs.some((run) => !terminal(run))) refreshRuns(); }, 2000);
})();
