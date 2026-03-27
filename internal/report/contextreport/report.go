package contextreport

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"techlog-stat/internal/config"
	"techlog-stat/internal/discovery"
	"techlog-stat/internal/model"
	"techlog-stat/internal/report/reportutil"
)

const toolVersion = "0.1.0"

type reportSpec struct {
	reportName       string
	eventNames       map[string]struct{}
	contextPattern   string
	contextEventName string
}

type eventHeader struct {
	EventName    string
	DurationMS   float64
	DurationUS   int64
	Timestamp    time.Time
	HourBucket   time.Time
	SourcePrefix string
}

type pendingCall struct {
	event model.RawContextEvent
}

type contextStat struct {
	durationMS float64
	count      int64
}

type aggregator struct {
	totalDurationMS float64
	totalCount      int64
	perContext      map[string]*contextStat
}

type parsedContextEvent struct {
	Timestamp      time.Time
	HourBucket     time.Time
	Event          string
	File           string
	DurationMicros int64
	DurationMS     float64
	Context        string
	ShortContext   string
}

type rawCollector struct {
	topN    int
	perHour map[time.Time][]model.RawContextEvent
}

type parseResult struct {
	processed bool
	bytesRead int64
	agg       aggregator
}

type rawParseResult struct {
	processed bool
	bytesRead int64
	raw       rawCollector
}

type fileResult struct {
	file   string
	result parseResult
	terr   error
}

type rawFileResult struct {
	file   string
	result rawParseResult
	terr   error
}

type parser struct {
	spec              reportSpec
	file              string
	fileHour          time.Time
	timeRange         config.TimeRange
	filters           []config.Filter
	minDurationMicros int64
	emit              func(parsedContextEvent)
	current           strings.Builder
	currentHeader     eventHeader
	hasCurrent        bool
	pendingCalls      map[string]pendingCall
}

func Build(cfg config.Config) (model.ContextReport, error) {
	spec, ok := specForReport(cfg.Report)
	if !ok {
		return model.ContextReport{}, fmt.Errorf("unsupported report: %s", cfg.Report)
	}

	startedAt := time.Now()
	files, err := discovery.Files(cfg.InputRoot, cfg.Glob)
	if err != nil {
		return model.ContextReport{}, err
	}

	report := model.ContextReport{
		Meta:    newRunMeta(cfg, startedAt, len(files)),
		Matches: files,
	}
	if len(files) == 0 {
		report.Errors = append(report.Errors, fmt.Sprintf("no input files matched pattern %q under %q", cfg.Glob, cfg.InputRoot))
		finalizeRunMeta(&report.Meta, startedAt)
		return report, nil
	}

	workers := normalizeWorkers(cfg.Workers)
	totalAgg := newAggregator()
	for result := range processFiles(files, workers, spec, cfg.Filters, cfg.MinDurationMicros, cfg.TimeRange) {
		report.Meta.BytesRead += result.result.bytesRead
		if result.terr != nil {
			report.Meta.FilesFailed++
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", result.file, result.terr))
			continue
		}
		if result.result.processed {
			report.Meta.FilesProcessed++
		}
		mergeAggregators(&totalAgg, result.result.agg)
	}

	report.Totals = buildTotals(totalAgg)
	report.Rows = buildRows(totalAgg, cfg.TopN)
	finalizeRunMeta(&report.Meta, startedAt)
	return report, nil
}

func BuildRaw(cfg config.Config) (model.RawContextReport, error) {
	spec, ok := specForReport(cfg.Report)
	if !ok {
		return model.RawContextReport{}, fmt.Errorf("unsupported report: %s", cfg.Report)
	}

	startedAt := time.Now()
	files, err := discovery.Files(cfg.InputRoot, cfg.Glob)
	if err != nil {
		return model.RawContextReport{}, err
	}

	report := model.RawContextReport{
		Meta:    newRunMeta(cfg, startedAt, len(files)),
		Matches: files,
	}
	if len(files) == 0 {
		report.Errors = append(report.Errors, fmt.Sprintf("no input files matched pattern %q under %q", cfg.Glob, cfg.InputRoot))
		finalizeRunMeta(&report.Meta, startedAt)
		return report, nil
	}

	workers := normalizeWorkers(cfg.Workers)
	merged := newRawCollector(cfg.TopN)
	for result := range processFilesRaw(files, workers, spec, cfg.Filters, cfg.MinDurationMicros, cfg.TimeRange, cfg.TopN) {
		report.Meta.BytesRead += result.result.bytesRead
		if result.terr != nil {
			report.Meta.FilesFailed++
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", result.file, result.terr))
			continue
		}
		if result.result.processed {
			report.Meta.FilesProcessed++
		}
		mergeRawCollectors(&merged, result.result.raw)
	}

	report.Days = buildRawContextDays(merged)
	finalizeRunMeta(&report.Meta, startedAt)
	return report, nil
}

func newRunMeta(cfg config.Config, startedAt time.Time, filesMatched int) model.RunMeta {
	return model.RunMeta{
		ToolVersion:  toolVersion,
		Report:       cfg.Report,
		StartedAt:    startedAt,
		InputRoot:    cfg.InputRoot,
		Glob:         cfg.Glob,
		OutputDir:    cfg.OutputDir,
		Workers:      cfg.Workers,
		TopN:         cfg.TopN,
		Formats:      append([]string(nil), cfg.Formats...),
		FilesMatched: filesMatched,
	}
}

func finalizeRunMeta(meta *model.RunMeta, startedAt time.Time) {
	meta.FinishedAt = time.Now()
	meta.Duration = meta.FinishedAt.Sub(startedAt)
}

func normalizeWorkers(workers int) int {
	if workers < 1 {
		return 1
	}
	return workers
}

func specForReport(report string) (reportSpec, bool) {
	switch report {
	case config.ReportSDBLContext:
		return reportSpec{reportName: report, eventNames: makeEventSet("SDBL"), contextPattern: "Context="}, true
	case config.ReportCALLContext:
		return reportSpec{reportName: report, eventNames: makeEventSet("CALL"), contextPattern: "Context=", contextEventName: "Context"}, true
	case config.ReportDBMSSQLContext:
		return reportSpec{reportName: report, eventNames: makeEventSet("DBMSSQL"), contextPattern: "Context="}, true
	case config.ReportPostgresContext:
		return reportSpec{reportName: report, eventNames: makeEventSet("DBPOSTGRS"), contextPattern: "Context="}, true
	case config.ReportFileDBContext:
		return reportSpec{reportName: report, eventNames: makeEventSet("DBV8DBEng"), contextPattern: "Context="}, true
	case config.ReportLockContext:
		return reportSpec{reportName: report, eventNames: makeEventSet("TLOCK", "TTIMEOUT", "TDEADLOCK"), contextPattern: "Context="}, true
	case config.ReportTimeoutContext:
		return reportSpec{reportName: report, eventNames: makeEventSet("TTIMEOUT"), contextPattern: "Context="}, true
	case config.ReportDeadlockContext:
		return reportSpec{reportName: report, eventNames: makeEventSet("TDEADLOCK"), contextPattern: "Context="}, true
	default:
		return reportSpec{}, false
	}
}

func makeEventSet(names ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		out[name] = struct{}{}
	}
	return out
}

func processFiles(files []string, workers int, spec reportSpec, filters []config.Filter, minDurationMicros int64, timeRange config.TimeRange) <-chan fileResult {
	jobs := make(chan string)
	results := make(chan fileResult, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				result, err := processFileAggregate(file, spec, filters, minDurationMicros, timeRange)
				results <- fileResult{file: file, result: result, terr: err}
			}
		}()
	}

	go func() {
		for _, file := range files {
			jobs <- file
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	return results
}

func processFilesRaw(files []string, workers int, spec reportSpec, filters []config.Filter, minDurationMicros int64, timeRange config.TimeRange, topN int) <-chan rawFileResult {
	jobs := make(chan string)
	results := make(chan rawFileResult, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				result, err := processFileRaw(file, spec, filters, minDurationMicros, timeRange, topN)
				results <- rawFileResult{file: file, result: result, terr: err}
			}
		}()
	}

	go func() {
		for _, file := range files {
			jobs <- file
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	return results
}

func processFileAggregate(path string, spec reportSpec, filters []config.Filter, minDurationMicros int64, timeRange config.TimeRange) (parseResult, error) {
	agg := newAggregator()
	result, err := processFile(path, spec, filters, minDurationMicros, timeRange, func(event parsedContextEvent) {
		agg.add(event.Context, event.DurationMS)
	})
	result.agg = agg
	return result, err
}

func processFileRaw(path string, spec reportSpec, filters []config.Filter, minDurationMicros int64, timeRange config.TimeRange, topN int) (rawParseResult, error) {
	collector := newRawCollector(topN)
	baseResult, err := processFile(path, spec, filters, minDurationMicros, timeRange, func(event parsedContextEvent) {
		collector.collect(event)
	})
	return rawParseResult{processed: baseResult.processed, bytesRead: baseResult.bytesRead, raw: collector}, err
}

func processFile(path string, spec reportSpec, filters []config.Filter, minDurationMicros int64, timeRange config.TimeRange, emit func(parsedContextEvent)) (parseResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return parseResult{}, err
	}
	defer file.Close()

	fileHour, err := reportutil.ParseFileHour(path)
	if err != nil {
		return parseResult{}, err
	}

	reader := bufio.NewReaderSize(file, 4*1024*1024)
	p := parser{
		spec:              spec,
		file:              path,
		fileHour:          fileHour,
		timeRange:         timeRange,
		filters:           filters,
		minDurationMicros: minDurationMicros,
		emit:              emit,
		pendingCalls:      make(map[string]pendingCall),
	}
	var bytesRead int64

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			bytesRead += int64(len(line))
			p.consumeLine(line)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return parseResult{bytesRead: bytesRead}, err
		}
	}

	p.finishEvent()
	return parseResult{processed: true, bytesRead: bytesRead}, nil
}

func newAggregator() aggregator {
	return aggregator{perContext: make(map[string]*contextStat, 1024)}
}

func (a *aggregator) add(context string, durMS float64) {
	a.totalDurationMS += durMS
	a.totalCount++
	stat := a.perContext[context]
	if stat == nil {
		stat = &contextStat{}
		a.perContext[context] = stat
	}
	stat.durationMS += durMS
	stat.count++
}

func mergeAggregators(dst *aggregator, src aggregator) {
	dst.totalDurationMS += src.totalDurationMS
	dst.totalCount += src.totalCount
	for context, stat := range src.perContext {
		dstStat := dst.perContext[context]
		if dstStat == nil {
			dstStat = &contextStat{}
			dst.perContext[context] = dstStat
		}
		dstStat.durationMS += stat.durationMS
		dstStat.count += stat.count
	}
}

func newRawCollector(topN int) rawCollector {
	return rawCollector{topN: topN, perHour: make(map[time.Time][]model.RawContextEvent)}
}

func (c *rawCollector) collect(event parsedContextEvent) {
	row := model.RawContextEvent{
		Timestamp:      event.Timestamp,
		HourBucket:     event.HourBucket,
		Event:          event.Event,
		File:           event.File,
		DurationMicros: event.DurationMicros,
		DurationMS:     event.DurationMS,
		Context:        event.Context,
		ShortContext:   event.ShortContext,
	}
	events := append(c.perHour[event.HourBucket], row)
	sortRawContextEvents(events)
	if c.topN > 0 && len(events) > c.topN {
		events = events[:c.topN]
	}
	c.perHour[event.HourBucket] = events
}

func mergeRawCollectors(dst *rawCollector, src rawCollector) {
	for hour, events := range src.perHour {
		merged := append(dst.perHour[hour], events...)
		sortRawContextEvents(merged)
		if dst.topN > 0 && len(merged) > dst.topN {
			merged = merged[:dst.topN]
		}
		dst.perHour[hour] = merged
	}
}

func sortRawContextEvents(events []model.RawContextEvent) {
	sort.Slice(events, func(i, j int) bool {
		if events[i].DurationMicros == events[j].DurationMicros {
			if events[i].Timestamp.Equal(events[j].Timestamp) {
				if events[i].Context == events[j].Context {
					return events[i].File < events[j].File
				}
				return events[i].Context < events[j].Context
			}
			return events[i].Timestamp.Before(events[j].Timestamp)
		}
		return events[i].DurationMicros > events[j].DurationMicros
	})
}

func buildRawContextDays(collector rawCollector) []model.RawContextDay {
	if len(collector.perHour) == 0 {
		return nil
	}
	hours := make([]time.Time, 0, len(collector.perHour))
	for hour := range collector.perHour {
		hours = append(hours, hour)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i].Before(hours[j]) })

	dayMap := make(map[string][]model.RawContextHour, len(hours))
	dayOrder := make([]string, 0, len(hours))
	seen := make(map[string]bool, len(hours))
	for _, hour := range hours {
		dayKey := reportutil.DayKey(hour)
		if !seen[dayKey] {
			seen[dayKey] = true
			dayOrder = append(dayOrder, dayKey)
		}
		dayMap[dayKey] = append(dayMap[dayKey], model.RawContextHour{Hour: hour, Events: collector.perHour[hour]})
	}

	days := make([]model.RawContextDay, 0, len(dayOrder))
	for _, dayKey := range dayOrder {
		hours := dayMap[dayKey]
		sort.Slice(hours, func(i, j int) bool { return hours[i].Hour.Before(hours[j].Hour) })
		days = append(days, model.RawContextDay{Date: dayKey, Hours: hours})
	}
	return days
}

func (p *parser) consumeLine(raw []byte) {
	line := normalizeLine(raw)
	if line == "" {
		return
	}

	if isEventStart(line) {
		p.finishEvent()
		header, ok := parseEventHeader(line, p.fileHour)
		if !ok {
			return
		}
		p.currentHeader = header
		p.hasCurrent = true
		p.current.WriteString(line)
		return
	}

	if !p.hasCurrent {
		return
	}
	if p.current.Len() > 0 {
		p.current.WriteByte(' ')
	}
	p.current.WriteString(line)
}

func (p *parser) finishEvent() {
	if !p.hasCurrent {
		p.resetCurrent()
		return
	}

	eventText := p.current.String()
	switch p.spec.reportName {
	case config.ReportCALLContext:
		p.finishCallContextEvent(p.currentHeader, eventText)
	default:
		if p.spec.matchesEvent(p.currentHeader.EventName) && p.matchesEventFilters(eventText, p.currentHeader) {
			context := extractContext(eventText, p.spec)
			if context != "" {
				p.emit(parsedContextEvent{
					Timestamp:      p.currentHeader.Timestamp,
					HourBucket:     p.currentHeader.HourBucket,
					Event:          p.currentHeader.EventName,
					File:           p.file,
					DurationMicros: p.currentHeader.DurationUS,
					DurationMS:     p.currentHeader.DurationMS,
					Context:        context,
					ShortContext:   reportutil.ShortenContext(context),
				})
			}
		}
	}

	p.resetCurrent()
}

func (p *parser) finishCallContextEvent(header eventHeader, eventText string) {
	thread := extractFieldValue(eventText, "OSThread=")
	if thread == "" {
		return
	}

	if p.spec.matchesEvent(header.EventName) {
		if p.matchesEventFilters(eventText, header) {
			p.pendingCalls[thread] = pendingCall{event: model.RawContextEvent{
				Timestamp:      header.Timestamp,
				HourBucket:     header.HourBucket,
				Event:          header.EventName,
				File:           p.file,
				DurationMicros: header.DurationUS,
				DurationMS:     header.DurationMS,
			}}
		}
		return
	}

	if header.EventName != p.spec.contextEventName {
		return
	}
	pending, ok := p.pendingCalls[thread]
	if !ok {
		return
	}
	context := extractContext(eventText, p.spec)
	if context == "" {
		return
	}
	p.emit(parsedContextEvent{
		Timestamp:      pending.event.Timestamp,
		HourBucket:     pending.event.HourBucket,
		Event:          pending.event.Event,
		File:           pending.event.File,
		DurationMicros: pending.event.DurationMicros,
		DurationMS:     pending.event.DurationMS,
		Context:        context,
		ShortContext:   reportutil.ShortenContext(context),
	})
	delete(p.pendingCalls, thread)
}

func (p *parser) matchesEventFilters(event string, header eventHeader) bool {
	if header.DurationUS < p.minDurationMicros {
		return false
	}
	if !p.timeRange.Match(header.Timestamp) {
		return false
	}
	return config.MatchAllFilters(event, p.filters)
}

func (p *parser) resetCurrent() {
	p.current.Reset()
	p.currentHeader = eventHeader{}
	p.hasCurrent = false
}

func (s reportSpec) matchesEvent(name string) bool {
	_, ok := s.eventNames[name]
	return ok
}

func buildTotals(agg aggregator) model.Totals {
	totals := model.Totals{EventCount: agg.totalCount, DurationMS: agg.totalDurationMS}
	if agg.totalCount > 0 {
		totals.AverageDuration = agg.totalDurationMS / float64(agg.totalCount)
	}
	return totals
}

func buildRows(agg aggregator, topN int) []model.ContextRow {
	rows := make([]model.ContextRow, 0, len(agg.perContext))
	for context, stat := range agg.perContext {
		row := model.ContextRow{
			Context:         context,
			ShortContext:    reportutil.ShortenContext(context),
			TotalDurationMS: stat.durationMS,
			Count:           stat.count,
		}
		if agg.totalDurationMS > 0 {
			row.TimePct = stat.durationMS / agg.totalDurationMS * 100
		}
		if agg.totalCount > 0 {
			row.CountPct = float64(stat.count) / float64(agg.totalCount) * 100
		}
		if stat.count > 0 {
			row.AverageMS = stat.durationMS / float64(stat.count)
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TotalDurationMS == rows[j].TotalDurationMS {
			return rows[i].Context < rows[j].Context
		}
		return rows[i].TotalDurationMS > rows[j].TotalDurationMS
	})
	if topN > 0 && len(rows) > topN {
		rows = rows[:topN]
	}
	for i := range rows {
		rows[i].Rank = i + 1
	}
	return rows
}

func normalizeLine(raw []byte) string {
	raw = bytes.TrimRight(raw, "\r\n")
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if len(raw) == 0 {
		return ""
	}
	return collapseWhitespace(string(raw))
}

func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if prevSpace {
				continue
			}
			b.WriteByte(' ')
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

func isEventStart(line string) bool {
	if len(line) < 12 {
		return false
	}
	return isDigit(line[0]) && isDigit(line[1]) && line[2] == ':' && isDigit(line[3]) && isDigit(line[4]) && line[5] == '.'
}

func parseEventHeader(line string, fileHour time.Time) (eventHeader, bool) {
	comma1 := strings.IndexByte(line, ',')
	if comma1 <= 0 {
		return eventHeader{}, false
	}
	prefix := line[:comma1]
	minus := strings.LastIndexByte(prefix, '-')
	if minus <= 0 || minus+1 >= len(prefix) {
		return eventHeader{}, false
	}
	timePart := prefix[:minus]
	durMicros, err := strconv.ParseInt(prefix[minus+1:], 10, 64)
	if err != nil {
		return eventHeader{}, false
	}
	if len(timePart) < len("00:00.000000") || timePart[2] != ':' || timePart[5] != '.' {
		return eventHeader{}, false
	}
	minute, err := strconv.Atoi(timePart[0:2])
	if err != nil {
		return eventHeader{}, false
	}
	second, err := strconv.Atoi(timePart[3:5])
	if err != nil {
		return eventHeader{}, false
	}
	micros, err := strconv.Atoi(timePart[6:])
	if err != nil {
		return eventHeader{}, false
	}
	rest := line[comma1+1:]
	comma2 := strings.IndexByte(rest, ',')
	if comma2 <= 0 {
		return eventHeader{}, false
	}
	ts := reportutil.BuildTimestamp(fileHour, minute, second, micros)
	return eventHeader{EventName: rest[:comma2], DurationUS: durMicros, DurationMS: float64(durMicros) / 1000.0, Timestamp: ts, HourBucket: fileHour, SourcePrefix: prefix}, true
}

func extractContext(event string, spec reportSpec) string {
	idx := strings.Index(event, spec.contextPattern)
	if idx < 0 {
		return ""
	}
	ctx := strings.TrimSpace(event[idx+len(spec.contextPattern):])
	if len(spec.eventNames) == 1 {
		if _, ok := spec.eventNames["CALL"]; ok {
			if end := strings.Index(ctx, ",Interface="); end >= 0 {
				ctx = ctx[:end]
			}
		}
	}
	ctx = strings.TrimSpace(ctx)
	ctx = strings.Trim(ctx, "'\"")
	return ctx
}

func extractFieldValue(event string, field string) string {
	value, ok := config.ExtractFieldValue(event, strings.TrimSuffix(field, "="))
	if !ok {
		return ""
	}
	return value
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
