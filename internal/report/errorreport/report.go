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
)

const toolVersion = "0.1.0"

type reportSpec struct {
	reportName string
	eventNames map[string]struct{}
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

type parseResult struct {
	processed bool
	bytesRead int64
	agg       aggregator
}

type fileResult struct {
	file   string
	result parseResult
	terr   error
}

type parser struct {
	spec              reportSpec
	filters           []config.Filter
	minDurationMicros int64
	agg               *aggregator
	current           strings.Builder
	currentEventName  string
	currentDurMicros  int64
	currentDurMS      float64
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

	report := model.ErrorReport{
		Meta: model.RunMeta{
			ToolVersion:  toolVersion,
			Report:       cfg.Report,
			StartedAt:    startedAt,
			InputRoot:    cfg.InputRoot,
			Glob:         cfg.Glob,
			OutputDir:    cfg.OutputDir,
			Workers:      cfg.Workers,
			TopN:         cfg.TopN,
			Formats:      append([]string(nil), cfg.Formats...),
			FilesMatched: len(files),
		},
		Matches: files,
	}

	if len(files) == 0 {
		report.Errors = append(report.Errors, fmt.Sprintf("no input files matched pattern %q under %q", cfg.Glob, cfg.InputRoot))
		report.Meta.FinishedAt = time.Now()
		report.Meta.Duration = report.Meta.FinishedAt.Sub(startedAt)
		return report, nil
	}

	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}

	totalAgg := newAggregator()
	for result := range processFiles(files, workers, spec, cfg.Filters, cfg.MinDurationMicros) {
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
	report.Meta.FinishedAt = time.Now()
	report.Meta.Duration = report.Meta.FinishedAt.Sub(startedAt)
	return report, nil
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

func processFiles(files []string, workers int, spec reportSpec, filters []config.Filter, minDurationMicros int64) <-chan fileResult {
	jobs := make(chan string)
	results := make(chan fileResult, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				result, err := processFile(file, spec, filters, minDurationMicros)
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

func processFile(path string, spec reportSpec, filters []config.Filter, minDurationMicros int64) (parseResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return parseResult{}, err
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 4*1024*1024)
	agg := newAggregator()
	p := parser{spec: spec, filters: filters, minDurationMicros: minDurationMicros, agg: &agg}
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
	return parseResult{processed: true, bytesRead: bytesRead, agg: agg}, nil
}

func newAggregator() aggregator {
	return aggregator{perError: make(map[errorKey]*errorStat, 1024)}
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

func (p *parser) consumeLine(raw []byte) {
	line := normalizeLine(raw)
	if line == "" {
		return
	}

	if isEventStart(line) {
		p.finishEvent()
		eventName, durMicros, durMS, ok := parseEventHeader(line)
		if !ok {
			return
		}
		p.currentEventName = eventName
		p.currentDurMicros = durMicros
		p.currentDurMS = durMS
		p.current.WriteString(line)
		return
	}

	if p.currentEventName == "" {
		return
	}
	if p.current.Len() > 0 {
		p.current.WriteByte(' ')
	}
	p.current.WriteString(line)
}

func (p *parser) finishEvent() {
	if p.currentEventName == "" {
		p.current.Reset()
		p.currentDurMicros = 0
		p.currentDurMS = 0
		return
	}

	eventText := p.current.String()
	if p.spec.matchesEvent(p.currentEventName) && p.matchesEventFilters(eventText, p.currentDurMicros) {
		descr := extractDescription(eventText)
		if descr != "" {
			p.agg.add(p.currentEventName, descr, p.currentDurMS)
		}
	}

	p.current.Reset()
	p.currentEventName = ""
	p.currentDurMicros = 0
	p.currentDurMS = 0
}

func (p *parser) matchesEventFilters(event string, durMicros int64) bool {
	if durMicros < p.minDurationMicros {
		return false
	}
	return config.MatchAllFilters(event, p.filters)
}

func (s reportSpec) matchesEvent(name string) bool {
	_, ok := s.eventNames[name]
	return ok
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
		row := model.ErrorRow{
			Event:            key.event,
			Description:      key.descr,
			ShortDescription: shortenDescription(key.descr),
			TotalDurationMS:  stat.durationMS,
			Count:            stat.count,
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

func parseEventHeader(line string) (string, int64, float64, bool) {
	comma1 := strings.IndexByte(line, ',')
	if comma1 <= 0 {
		return "", 0, 0, false
	}
	prefix := line[:comma1]
	minus := strings.LastIndexByte(prefix, '-')
	if minus <= 0 || minus+1 >= len(prefix) {
		return "", 0, 0, false
	}
	durMicros, err := strconv.ParseInt(prefix[minus+1:], 10, 64)
	if err != nil {
		return "", 0, 0, false
	}
	rest := line[comma1+1:]
	comma2 := strings.IndexByte(rest, ',')
	if comma2 <= 0 {
		return "", 0, 0, false
	}
	return rest[:comma2], durMicros, float64(durMicros) / 1000.0, true
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

func shortenDescription(descr string) string {
	descr = strings.TrimSpace(descr)
	if descr == "" {
		return ""
	}
	const maxLen = 160
	runes := []rune(descr)
	if len(runes) <= maxLen {
		return descr
	}
	return strings.TrimSpace(string(runes[:maxLen])) + "..."
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
