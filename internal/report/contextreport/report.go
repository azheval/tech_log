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
)

const toolVersion = "0.1.0"

type reportSpec struct {
	reportName       string
	eventNames       map[string]struct{}
	contextPattern   string
	contextEventName string
}

type pendingCall struct {
	durationMS float64
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
	spec             reportSpec
	agg              *aggregator
	current          strings.Builder
	currentEventName string
	currentDurMS     float64
	pendingCalls     map[string]pendingCall
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
	for result := range processFiles(files, workers, spec) {
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

func processFiles(files []string, workers int, spec reportSpec) <-chan fileResult {
	jobs := make(chan string)
	results := make(chan fileResult, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				result, err := processFile(file, spec)
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

func processFile(path string, spec reportSpec) (parseResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return parseResult{}, err
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 4*1024*1024)
	agg := newAggregator()
	p := parser{spec: spec, agg: &agg, pendingCalls: make(map[string]pendingCall)}
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
	return aggregator{perContext: make(map[string]*contextStat, 1024)}
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

func (p *parser) consumeLine(raw []byte) {
	line := normalizeLine(raw)
	if line == "" {
		return
	}

	if isEventStart(line) {
		p.finishEvent()
		eventName, durMS, ok := parseEventHeader(line)
		if !ok {
			return
		}
		p.currentEventName = eventName
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
		p.currentDurMS = 0
		return
	}

	eventText := p.current.String()
	switch p.spec.reportName {
	case config.ReportCALLContext:
		p.finishCallContextEvent(p.currentEventName, p.currentDurMS, eventText)
	default:
		if p.spec.matchesEvent(p.currentEventName) {
			context := extractContext(eventText, p.spec)
			if context != "" {
				p.agg.add(context, p.currentDurMS)
			}
		}
	}

	p.current.Reset()
	p.currentEventName = ""
	p.currentDurMS = 0
}

func (p *parser) finishCallContextEvent(eventName string, durMS float64, eventText string) {
	thread := extractFieldValue(eventText, "OSThread=")
	if thread == "" {
		return
	}

	if p.spec.matchesEvent(eventName) {
		p.pendingCalls[thread] = pendingCall{durationMS: durMS}
		return
	}

	if eventName != p.spec.contextEventName {
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
	p.agg.add(context, pending.durationMS)
	delete(p.pendingCalls, thread)
}

func (s reportSpec) matchesEvent(name string) bool {
	_, ok := s.eventNames[name]
	return ok
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
			ShortContext:    shortenContext(context),
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

func parseEventHeader(line string) (string, float64, bool) {
	comma1 := strings.IndexByte(line, ',')
	if comma1 <= 0 {
		return "", 0, false
	}
	prefix := line[:comma1]
	minus := strings.LastIndexByte(prefix, '-')
	if minus <= 0 || minus+1 >= len(prefix) {
		return "", 0, false
	}
	dur, err := strconv.ParseFloat(prefix[minus+1:], 64)
	if err != nil {
		return "", 0, false
	}
	rest := line[comma1+1:]
	comma2 := strings.IndexByte(rest, ',')
	if comma2 <= 0 {
		return "", 0, false
	}
	return rest[:comma2], dur / 1000.0, true
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
	idx := strings.Index(event, field)
	if idx < 0 {
		return ""
	}
	value := event[idx+len(field):]
	if end := strings.IndexByte(value, ','); end >= 0 {
		value = value[:end]
	}
	return strings.TrimSpace(value)
}

func shortenContext(context string) string {
	context = strings.TrimSpace(context)
	if context == "" {
		return ""
	}
	if idx := strings.Index(context, " ; "); idx >= 0 {
		return strings.TrimSpace(context[:idx])
	}
	if idx := strings.Index(context, "; "); idx >= 0 {
		return strings.TrimSpace(context[:idx])
	}
	const maxLen = 160
	runes := []rune(context)
	if len(runes) <= maxLen {
		return context
	}
	return strings.TrimSpace(string(runes[:maxLen])) + "..."
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
