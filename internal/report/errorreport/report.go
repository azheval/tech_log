package errorreport

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
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
	reportName string
	eventNames map[string]struct{}
}

type eventHeader struct {
	EventName  string
	DurationUS int64
	DurationMS float64
	Timestamp  time.Time
	HourBucket time.Time
}

type errorKey struct {
	event string
	descr string
}

type errorStat struct {
	durationMS float64
	count      int64
}

type aggregator struct {
	totalDurationMS float64
	totalCount      int64
	perError        map[errorKey]*errorStat
}

type parsedErrorEvent struct {
	Timestamp        time.Time
	HourBucket       time.Time
	Event            string
	File             string
	DurationMicros   int64
	DurationMS       float64
	Description      string
	ShortDescription string
}

type rawCollector struct {
	topN    int
	perHour map[time.Time][]model.RawErrorEvent
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
	emit              func(parsedErrorEvent)
	current           strings.Builder
	currentHeader     eventHeader
	hasCurrent        bool
}

var (
	reIPv6 = regexp.MustCompile(`\[[\w:]+%?\d*\]:\w+`)
	reIPv4 = regexp.MustCompile(`\d+\.\d+\.\d+\.\d+:\d+`)
	reUUID = regexp.MustCompile(`[\w\d]{8}-[\w\d]{4}-[\w\d]{4}-[\w\d]{4}-[\w\d]{12}`)
	reDtTm = regexp.MustCompile(`\\x{043D}\\x{0430}\\x{0447}\\x{0430}\\x{0442}:(\\s+\\d\\d\\.\\d\\d\\.\\d{4}\\s+\\x{0432}\\s+\\d+:\\d+:\\d+)`)
)

func Build(cfg config.Config) (model.ErrorReport, error) {
	spec, ok := specForReport(cfg.Report)
	if !ok {
		return model.ErrorReport{}, fmt.Errorf("unsupported report: %s", cfg.Report)
	}

	startedAt := time.Now()
	files, err := discovery.Files(cfg.InputRoot, cfg.Glob)
	if err != nil {
		return model.ErrorReport{}, err
	}

	report := model.ErrorReport{Meta: newRunMeta(cfg, startedAt, len(files)), Matches: files}
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

func BuildRaw(cfg config.Config) (model.RawErrorReport, error) {
	spec, ok := specForReport(cfg.Report)
	if !ok {
		return model.RawErrorReport{}, fmt.Errorf("unsupported report: %s", cfg.Report)
	}

	startedAt := time.Now()
	files, err := discovery.Files(cfg.InputRoot, cfg.Glob)
	if err != nil {
		return model.RawErrorReport{}, err
	}

	report := model.RawErrorReport{Meta: newRunMeta(cfg, startedAt, len(files)), Matches: files}
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

	report.Days = buildRawErrorDays(merged)
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
	case config.ReportErrorDescr:
		return reportSpec{reportName: report, eventNames: makeEventSet("EXCP", "QERR")}, true
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
	base, err := processFile(path, spec, filters, minDurationMicros, timeRange, func(event parsedErrorEvent) {
		agg.add(event.Event, event.Description, event.DurationMS)
	})
	base.agg = agg
	return base, err
}

func processFileRaw(path string, spec reportSpec, filters []config.Filter, minDurationMicros int64, timeRange config.TimeRange, topN int) (rawParseResult, error) {
	collector := newRawCollector(topN)
	base, err := processFile(path, spec, filters, minDurationMicros, timeRange, func(event parsedErrorEvent) {
		collector.collect(event)
	})
	return rawParseResult{processed: base.processed, bytesRead: base.bytesRead, raw: collector}, err
}

func processFile(path string, spec reportSpec, filters []config.Filter, minDurationMicros int64, timeRange config.TimeRange, emit func(parsedErrorEvent)) (parseResult, error) {
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
	p := parser{spec: spec, file: path, fileHour: fileHour, timeRange: timeRange, filters: filters, minDurationMicros: minDurationMicros, emit: emit}
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
	return aggregator{perError: make(map[errorKey]*errorStat, 1024)}
}

func (a *aggregator) add(eventName, descr string, durMS float64) {
	a.totalDurationMS += durMS
	a.totalCount++
	key := errorKey{event: eventName, descr: descr}
	stat := a.perError[key]
	if stat == nil {
		stat = &errorStat{}
		a.perError[key] = stat
	}
	stat.durationMS += durMS
	stat.count++
}

func mergeAggregators(dst *aggregator, src aggregator) {
	dst.totalDurationMS += src.totalDurationMS
	dst.totalCount += src.totalCount
	for key, stat := range src.perError {
		dstStat := dst.perError[key]
		if dstStat == nil {
			dstStat = &errorStat{}
			dst.perError[key] = dstStat
		}
		dstStat.durationMS += stat.durationMS
		dstStat.count += stat.count
	}
}

func newRawCollector(topN int) rawCollector {
	return rawCollector{topN: topN, perHour: make(map[time.Time][]model.RawErrorEvent)}
}

func (c *rawCollector) collect(event parsedErrorEvent) {
	row := model.RawErrorEvent{
		Timestamp:        event.Timestamp,
		HourBucket:       event.HourBucket,
		Event:            event.Event,
		File:             event.File,
		DurationMicros:   event.DurationMicros,
		DurationMS:       event.DurationMS,
		Description:      event.Description,
		ShortDescription: event.ShortDescription,
	}
	events := append(c.perHour[event.HourBucket], row)
	sortRawErrorEvents(events)
	if c.topN > 0 && len(events) > c.topN {
		events = events[:c.topN]
	}
	c.perHour[event.HourBucket] = events
}

func mergeRawCollectors(dst *rawCollector, src rawCollector) {
	for hour, events := range src.perHour {
		merged := append(dst.perHour[hour], events...)
		sortRawErrorEvents(merged)
		if dst.topN > 0 && len(merged) > dst.topN {
			merged = merged[:dst.topN]
		}
		dst.perHour[hour] = merged
	}
}

func sortRawErrorEvents(events []model.RawErrorEvent) {
	sort.Slice(events, func(i, j int) bool {
		if events[i].DurationMicros == events[j].DurationMicros {
			if events[i].Timestamp.Equal(events[j].Timestamp) {
				if events[i].Description == events[j].Description {
					return events[i].File < events[j].File
				}
				return events[i].Description < events[j].Description
			}
			return events[i].Timestamp.Before(events[j].Timestamp)
		}
		return events[i].DurationMicros > events[j].DurationMicros
	})
}

func buildRawErrorDays(collector rawCollector) []model.RawErrorDay {
	if len(collector.perHour) == 0 {
		return nil
	}
	hours := make([]time.Time, 0, len(collector.perHour))
	for hour := range collector.perHour {
		hours = append(hours, hour)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i].Before(hours[j]) })
	dayMap := make(map[string][]model.RawErrorHour, len(hours))
	dayOrder := make([]string, 0, len(hours))
	seen := make(map[string]bool, len(hours))
	for _, hour := range hours {
		dayKey := reportutil.DayKey(hour)
		if !seen[dayKey] {
			seen[dayKey] = true
			dayOrder = append(dayOrder, dayKey)
		}
		dayMap[dayKey] = append(dayMap[dayKey], model.RawErrorHour{Hour: hour, Events: collector.perHour[hour]})
	}
	days := make([]model.RawErrorDay, 0, len(dayOrder))
	for _, dayKey := range dayOrder {
		hours := dayMap[dayKey]
		sort.Slice(hours, func(i, j int) bool { return hours[i].Hour.Before(hours[j].Hour) })
		days = append(days, model.RawErrorDay{Date: dayKey, Hours: hours})
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
	if p.spec.matchesEvent(p.currentHeader.EventName) && p.matchesEventFilters(eventText, p.currentHeader) {
		descr := extractDescription(eventText)
		if descr != "" {
			p.emit(parsedErrorEvent{
				Timestamp:        p.currentHeader.Timestamp,
				HourBucket:       p.currentHeader.HourBucket,
				Event:            p.currentHeader.EventName,
				File:             p.file,
				DurationMicros:   p.currentHeader.DurationUS,
				DurationMS:       p.currentHeader.DurationMS,
				Description:      descr,
				ShortDescription: reportutil.ShortenDescription(descr),
			})
		}
	}
	p.resetCurrent()
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

func buildRows(agg aggregator, topN int) []model.ErrorRow {
	rows := make([]model.ErrorRow, 0, len(agg.perError))
	for key, stat := range agg.perError {
		row := model.ErrorRow{Event: key.event, Description: key.descr, ShortDescription: reportutil.ShortenDescription(key.descr), TotalDurationMS: stat.durationMS, Count: stat.count}
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
		if rows[i].Count == rows[j].Count {
			if rows[i].TotalDurationMS == rows[j].TotalDurationMS {
				if rows[i].Event == rows[j].Event {
					return rows[i].Description < rows[j].Description
				}
				return rows[i].Event < rows[j].Event
			}
			return rows[i].TotalDurationMS > rows[j].TotalDurationMS
		}
		return rows[i].Count > rows[j].Count
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
	return eventHeader{EventName: rest[:comma2], DurationUS: durMicros, DurationMS: float64(durMicros) / 1000.0, Timestamp: ts, HourBucket: fileHour}, true
}

func extractDescription(event string) string {
	descr := extractTailValue(event, "Descr=")
	if descr == "" {
		descr = extractTailValue(event, "Description=")
	}
	if descr == "" {
		return ""
	}
	descr = strings.Trim(descr, "'\"")
	descr = normalizeDescription(descr)
	return strings.TrimSpace(descr)
}

func extractTailValue(event string, field string) string {
	idx := strings.Index(event, field)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(event[idx+len(field):])
}

func normalizeDescription(descr string) string {
	descr = reIPv6.ReplaceAllString(descr, "{IPV6}")
	descr = reIPv4.ReplaceAllString(descr, "{IPV4}")
	descr = reUUID.ReplaceAllString(descr, "{UUID}")
	descr = reDtTm.ReplaceAllString(descr, "{DtTm}")
	return descr
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
