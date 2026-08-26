package overview

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"techlog-stat/internal/config"
)

func TestEmptyOverviewIsJSONSerializable(t *testing.T) {
	dir := t.TempDir()
	result, err := Build(Options{InputRoot: dir, Glob: "*.log", BucketInterval: time.Minute, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Totals.Count != 0 || result.Totals.Duration.Sum != 0 {
		t.Fatalf("unexpected totals: %+v", result.Totals)
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("empty overview must be JSON serializable: %v", err)
	}
}

func TestBuildAggregatesFilesInOneOverview(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, filepath.Join(dir, "26082310.log"),
		"orphan line\n"+
			"00:02.000000-100,SDBL,1,Usr=alice,DataBase=main,process=rphost,Context=First\n"+
			"06:07.000000-300,CALL,1,Usr=bob,DataBase=main,process=rphost,Context=Second\n"+
			"00:09.000000-nope,SDBL,1,Usr=alice\n")
	writeLog(t, filepath.Join(dir, "26082311.log"),
		"00:00.000000-200,SDBL,1,Usr=alice,DataBase=archive,process=rmngr\n"+
			"00:03.000000-500,EXCP,2,Descr='boom'\n")

	result, err := Build(Options{InputRoot: dir, Glob: "*.log", BucketInterval: 5 * time.Minute, TopN: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta.FilesMatched != 2 || result.Meta.FilesProcessed != 2 || result.Meta.FilesFailed != 0 {
		t.Fatalf("meta = %+v", result.Meta)
	}
	if result.Quality.EventsParsed != 4 || result.Quality.MalformedHeaders != 1 || result.Quality.OrphanLines != 1 {
		t.Fatalf("quality = %+v", result.Quality)
	}
	if result.Totals.Count != 4 || result.Totals.Duration.Sum != 1100 || result.Totals.Duration.Min != 100 || result.Totals.Duration.Max != 500 {
		t.Fatalf("totals = %+v", result.Totals)
	}
	if math.Abs(result.Totals.Duration.P50-250) > 1e-9 {
		t.Fatalf("P50 = %v, want 250", result.Totals.Duration.P50)
	}

	if got, want := eventNames(result), []string{"EXCP", "SDBL", "CALL"}; !equalStrings(got, want) {
		t.Fatalf("event order = %v, want %v", got, want)
	}
	if result.EventTypes[1].Stats.Count != 2 || result.EventTypes[1].Stats.Duration.Sum != 300 {
		t.Fatalf("SDBL stats = %+v", result.EventTypes[1].Stats)
	}
	if len(result.Buckets) != 3 || result.Buckets[0].Stats.Count != 1 || result.Buckets[1].Stats.Count != 1 || result.Buckets[2].Stats.Count != 2 {
		t.Fatalf("buckets = %+v", result.Buckets)
	}
	if got, want := dimensionNames(result.Users), []string{"(unknown)", "alice", "bob"}; !equalStrings(got, want) {
		t.Fatalf("users = %v, want %v", got, want)
	}
	if result.Users[1].Stats.Duration.Sum != 300 || result.Databases[0].Value != "(unknown)" || result.Processes[0].Value != "(unknown)" {
		t.Fatalf("dimensions: users=%+v databases=%+v processes=%+v", result.Users, result.Databases, result.Processes)
	}
	if got, want := dimensionNames(result.Contexts), []string{"(unknown)", "Second", "First"}; !equalStrings(got, want) {
		t.Fatalf("contexts = %v, want %v", got, want)
	}
	if got, want := rawDurations(result), []int64{500, 300, 200}; !equalInt64(got, want) {
		t.Fatalf("top durations = %v, want %v", got, want)
	}
	if result.TopEvents[0].Raw == "" || result.TopEvents[0].Fields["Descr"] != "boom" {
		t.Fatalf("raw top event = %+v", result.TopEvents[0])
	}
}

func TestBuildRecordsBadFileAndContinues(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, filepath.Join(dir, "26082310.log"), "00:00.000000-10,SDBL,1,Usr=alice\n")
	writeLog(t, filepath.Join(dir, "not-a-log.log"), "00:00.000000-20,SDBL,1,Usr=bob\n")

	result, err := Build(Options{InputRoot: dir, Glob: "*.log", BucketInterval: time.Hour, TopN: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta.FilesProcessed != 1 || result.Meta.FilesFailed != 1 || len(result.Errors) != 1 {
		t.Fatalf("meta/errors = %+v / %v", result.Meta, result.Errors)
	}
	if result.Totals.Count != 1 || result.Totals.Duration.Sum != 10 {
		t.Fatalf("totals = %+v", result.Totals)
	}
}

func TestBuildFeedsSQLAndTraceCollectorsInSinglePass(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, filepath.Join(dir, "26082310.log"), ""+
		"00:00.000000-10,CALL,1,process=rphost,OSThread=7,CallID=call-1\n"+
		"00:00.000001-20,Context,1,process=rphost,OSThread=7,CallID=call-1,Context=Module.Run\n"+
		"00:00.000002-30,SDBL,1,process=rphost,OSThread=7,CallID=call-1,Sdbl='SELECT * FROM Items WHERE id=42',Rows=3\n")

	result, err := Build(Options{InputRoot: dir, Glob: "*.log", BucketInterval: time.Minute, TopN: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Quality.EventsParsed != 3 || result.Totals.Count != 3 {
		t.Fatalf("overview did not receive all events: quality=%+v totals=%+v", result.Quality, result.Totals)
	}
	if len(result.SQLRows) != 1 || result.SQLRows[0].EventType != "SDBL" || result.SQLRows[0].Count != 1 || result.SQLRows[0].NormalizedQuery != "SELECT * FROM Items WHERE id=?" {
		t.Fatalf("SQLRows = %+v", result.SQLRows)
	}
	if len(result.Traces) != 1 || len(result.Traces[0].Spans) != 3 {
		t.Fatalf("Traces = %+v", result.Traces)
	}
	if result.TraceQuality.EventsConsumed != 3 || result.TraceQuality.CorrelatedEvents != 2 || result.TraceQuality.Contexts != 1 {
		t.Fatalf("TraceQuality = %+v", result.TraceQuality)
	}
}

func TestBuildFeedsLockCollectorInSinglePass(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, filepath.Join(dir, "26082310.log"), ""+
		"00:00.000000-10,TLOCK,1,Context=Posting,Locks='Table=Document.Sales; Region=Header\nTable=Catalog.Items; Region=Line'\n"+
		"00:00.000001-20,TTIMEOUT,1,Context=Posting,Regions='Line; Item'\n"+
		"00:00.000002-30,TDEADLOCK,1,Context=Closing,Locks='Table=Document.Sales,Region=Header',Waiter=conn-a,Blocker=conn-b\n")

	result, err := Build(Options{InputRoot: dir, Glob: "*.log", BucketInterval: time.Minute, TopN: 10, LockSampleLimit: 2, LockTopConflicts: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Quality.EventsParsed != 3 || result.Locks.Quality.LockEvents != 3 {
		t.Fatalf("lock events not consumed: parse=%+v locks=%+v", result.Quality, result.Locks.Quality)
	}
	if len(result.Locks.ByEvent) != 3 || result.Locks.ByEvent[0].Key != "TDEADLOCK" || result.Locks.ByEvent[0].Stats.TotalMicros != 30 {
		t.Fatalf("lock event aggregates=%+v", result.Locks.ByEvent)
	}
	if len(result.Locks.ByTable) != 2 || result.Locks.ByTable[0].Key != "Document.Sales" || result.Locks.ByTable[0].Stats.Count != 2 {
		t.Fatalf("lock tables=%+v", result.Locks.ByTable)
	}
	if len(result.Locks.TopConflicts) != 2 || len(result.Locks.Samples) != 2 || len(result.Locks.Relations) != 1 || result.Locks.Relations[0].Waiter != "conn-a" {
		t.Fatalf("lock detail=%+v", result.Locks)
	}
}

func TestBuildFeedsAdditionalCollectorsAndKeepsSimilarEventNamesSeparate(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, filepath.Join(dir, "26082310.log"), ""+
		"00:00.000000-10,SCALL,1,process=rphost,Usr=alice,DataBase=main,Context=Document.Write,Interface=iface,IName=IObject,Method=4\n"+
		"00:00.000001-0,VRSREQUEST,1,process=web,OSThread=1,Method=GET,URI=/api/items/42\n"+
		"00:00.000003-0,VRSRESPONSE,1,process=web,OSThread=1,Status=200\n"+
		"00:00.000004-0,SESN,1,ID=session-1,Action=start,Usr=alice\n"+
		"00:00.000006-0,SESN,1,ID=session-1,Action=finish\n"+
		"00:00.000007-20,PROC,1,process=1cv8,OSThread=7\n"+
		"00:00.000008-5,SCOM,1,process=1cv8,Func='new ServerProcessData(id,RHostRoot,RHostRoot)'\n"+
		"00:00.000009-9,LIC,1,process=1cv8,Usr=alice,Func=getLicense,res=seize,txt='license acquired'\n"+
		"00:00.000010-1,HASP,1,process=1cv8,Usr=alice,Txt='Computer parameters from OS:\nOS_0: Linux'\n"+
		"00:00.000011-1,EXCP,1,process=rphost,OSThread=9,Exception=DatabaseException,Descr=failed\n"+
		"00:00.000012-1,EXCPCNTX,1,process=rphost,OSThread=9,SrcName=Module.Form\n"+
		"00:00.000013-1,SCALLX,1,process=rphost\n"+
		"00:00.000014-1,VRSREQUESTX,1,process=web,OSThread=1,Method=GET,URI=/must-not-match\n"+
		"00:00.000015-1,SESNX,1,ID=session-2,Action=start\n"+
		"00:00.000016-1,PROCX,1,process=1cv8\n"+
		"00:00.000017-1,LICX,1,Func=getLicense,res=seize\n"+
		"00:00.000018-1,EXCPX,1,Exception=not-an-error\n")

	result, err := Build(Options{
		InputRoot: dir, Glob: "*.log", BucketInterval: time.Minute, TopN: 20,
		SCALLSampleLimit: 1, WebSampleLimit: 1, SessionLimit: 1, SessionSampleLimit: 1,
		ProcessSampleLimit: 1, LicenseSampleLimit: 1, ErrorContextPendingLimit: 1, ErrorContextSampleLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SCALL.Quality.CallEvents != 1 || len(result.SCALL.ByCall) != 1 {
		t.Fatalf("SCALL = %+v", result.SCALL)
	}
	if result.Web.Quality.MatchedResponses != 1 || len(result.Web.Requests) != 1 || result.Web.Requests[0].URI != "/api/items/{id}" {
		t.Fatalf("web = %+v", result.Web)
	}
	if result.Sessions.Quality.CompletedSessions != 1 || len(result.Sessions.Sessions) != 1 || result.Sessions.Sessions[0].DurationMicros != 2 {
		t.Fatalf("sessions = %+v", result.Sessions)
	}
	if result.ProcessStats.Quality.PROCEvents != 1 || result.ProcessStats.Quality.SCOMEvents != 1 || len(result.ProcessStats.ExplicitProcessRelations) != 1 {
		t.Fatalf("process stats = %+v", result.ProcessStats)
	}
	if result.Licenses.Quality.LicenseEvents != 1 || result.Licenses.Quality.HASPEvents != 1 || len(result.Licenses.Licenses) != 1 || len(result.Licenses.HASP) != 1 {
		t.Fatalf("licenses = %+v", result.Licenses)
	}
	if result.ErrorContext.Quality.ErrorEvents != 1 || result.ErrorContext.Quality.MatchedContexts != 1 || len(result.ErrorContext.Errors) != 1 || result.ErrorContext.Errors[0].Module != "Module.Form" {
		t.Fatalf("error context = %+v", result.ErrorContext)
	}
	if result.SCALL.Quality.IgnoredEvents != result.Quality.EventsParsed-1 || result.Web.Quality.EventsConsumed != result.Quality.EventsParsed || result.Licenses.Quality.EventsConsumed != result.Quality.EventsParsed {
		t.Fatalf("collectors did not consume the shared event stream once: parse=%+v scall=%+v web=%+v licenses=%+v", result.Quality, result.SCALL.Quality, result.Web.Quality, result.Licenses.Quality)
	}
}

func TestBuildFeedsFileDBCollectorInSinglePass(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, filepath.Join(dir, "26082310.log"), ""+
		"00:00.000000-10,DBV8DBEng,1,Func=Read,tableName=Catalog.Items,CatName=Catalog,FileName='C:\\\\srv\\\\base.1cd',Rows=2,RowsAffected=0,Trans=tx-1,DataBase=main,process=rphost,Usr=alice\n"+
		"00:00.000001-30,DBV8DBEng,1,Func=Write,tableName=Catalog.Items,FileName=relative.1cd,RowsAffected=1,DataBase=main,process=rphost,Usr=alice,Error=failed\n"+
		"00:00.000002-40,DBV8DBEngX,1,Func=Read\n")

	result, err := Build(Options{InputRoot: dir, Glob: "*.log", BucketInterval: time.Minute, TopN: 10, FileDBSlowSampleLimit: 1, FileDBErrorSampleLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.FileDB.Quality.EventsConsumed != 3 || result.FileDB.Quality.DBV8DBEngEvents != 2 || result.FileDB.Quality.IgnoredEvents != 1 {
		t.Fatalf("file db quality = %+v", result.FileDB.Quality)
	}
	if len(result.FileDB.ByFunc) != 2 || result.FileDB.ByFunc[0].Key != "Write" || result.FileDB.ByFunc[0].Duration.TotalMicros != 30 {
		t.Fatalf("file db functions = %+v", result.FileDB.ByFunc)
	}
	if len(result.FileDB.ByFile) != 2 || result.FileDB.ByFile[1].Key != "<absolute-path>/base.1cd" {
		t.Fatalf("file db paths = %+v", result.FileDB.ByFile)
	}
	if len(result.FileDB.SlowSamples) != 1 || result.FileDB.SlowSamples[0].DurationMicros != 30 || len(result.FileDB.ErrorSamples) != 1 || result.FileDB.ErrorSamples[0].ErrorField != "Error" {
		t.Fatalf("file db samples = %+v", result.FileDB)
	}
}

func TestBuildWorkersProduceDeterministicResult(t *testing.T) {
	dir := t.TempDir()
	var first strings.Builder
	for index := 0; index < eventBatchSize+20; index++ {
		first.WriteString("00:00.000000-10,SDBL,1,Usr=alice,DataBase=main,process=rphost,Sdbl='SELECT * FROM Items WHERE id=1'\n")
	}
	writeLog(t, filepath.Join(dir, "26082310.log"), first.String())
	writeLog(t, filepath.Join(dir, "26082311.log"), "00:00.000000-20,CALL,1,process=rphost,OSThread=2,CallID=call-2\n00:00.000001-30,Context,1,process=rphost,OSThread=2,CallID=call-2\n")
	writeLog(t, filepath.Join(dir, "26082312.log"), "00:00.000000-40,TLOCK,1,Context=Posting,Locks='Table=Document.Sales; Region=Header'\n")
	writeLog(t, filepath.Join(dir, "26082313.log"), "00:00.000000-50,EXCP,1,Usr=bob,Descr='boom'\n")

	base := Options{InputRoot: dir, Glob: "*.log", BucketInterval: time.Minute, TopN: 10, Workers: 1, LockSampleLimit: 10, LockTopConflicts: 10}
	sequential, err := Build(base)
	if err != nil {
		t.Fatal(err)
	}
	parallelOptions := base
	parallelOptions.Workers = 3
	parallel, err := Build(parallelOptions)
	if err != nil {
		t.Fatal(err)
	}
	sequential.Meta = Meta{}
	parallel.Meta = Meta{}
	if !reflect.DeepEqual(sequential, parallel) {
		t.Fatalf("workers produced different results\nsequential=%+v\nparallel=%+v", sequential, parallel)
	}
}

func TestBuildContextReportsCumulativeProgress(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, filepath.Join(dir, "26082310.log"), "00:00.000000-10,SDBL,1,Usr=keep\n00:00.000001-20,SDBL,1,Usr=drop\n")
	writeLog(t, filepath.Join(dir, "26082311.log"), "00:00.000000-30,CALL,1,Usr=keep\n")

	var updates []Progress
	result, err := BuildContext(context.Background(), Options{
		InputRoot: dir, Glob: "*.log", BucketInterval: time.Minute, TopN: 10,
		Filters: []config.Filter{{Key: "Usr", Value: "keep"}},
		Progress: func(progress Progress) {
			updates = append(updates, progress)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) == 0 || updates[0].Status != "matched" || updates[0].FilesMatched != 2 {
		t.Fatalf("initial progress = %+v", updates)
	}
	last := updates[len(updates)-1]
	if last.Status != "completed" || last.FilesCompleted != 2 || last.FilesFailed != 0 || last.EventsParsed != 3 || last.EventsAccepted != 2 || last.BytesRead == 0 {
		t.Fatalf("final progress = %+v", last)
	}
	if result.Quality.EventsParsed != last.EventsParsed || result.Meta.FilesProcessed != last.FilesCompleted {
		t.Fatalf("result and progress disagree: result=%+v progress=%+v", result, last)
	}
	seenCurrentFile := false
	for _, update := range updates {
		if update.Status == "parsing" && update.CurrentFile != "" {
			seenCurrentFile = true
		}
	}
	if !seenCurrentFile {
		t.Fatalf("progress never identified a current file: %+v", updates)
	}
}

func TestBuildContextCancellationReturnsWithoutWorkerDeadlock(t *testing.T) {
	dir := t.TempDir()
	contents := strings.Repeat("00:00.000000-10,SDBL,1,Usr=alice\n", eventBatchSize*4)
	writeLog(t, filepath.Join(dir, "26082310.log"), contents)
	writeLog(t, filepath.Join(dir, "26082311.log"), contents)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := BuildContext(ctx, Options{
			InputRoot: dir, Glob: "*.log", BucketInterval: time.Minute, TopN: 1, Workers: 2,
			Progress: func(progress Progress) {
				if progress.EventsParsed > 0 {
					cancel()
				}
			},
		})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("BuildContext error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("BuildContext did not return after cancellation; a worker may be blocked")
	}
}

func TestBuildContextReturnsDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := BuildContext(ctx, Options{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("BuildContext error = %v, want context.DeadlineExceeded", err)
	}
}

func TestBuildMatchesBackgroundContextBuild(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, filepath.Join(dir, "26082310.log"), "00:00.000000-10,SDBL,1,Usr=alice\n")
	options := Options{InputRoot: dir, Glob: "*.log", BucketInterval: time.Minute, TopN: 10}

	legacy, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	contextual, err := BuildContext(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Meta = Meta{}
	contextual.Meta = Meta{}
	if !reflect.DeepEqual(legacy, contextual) {
		t.Fatalf("Build and BuildContext differ\nBuild=%+v\nBuildContext=%+v", legacy, contextual)
	}
}

func TestBuildAppliesFiltersBeforeEveryCollector(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, filepath.Join(dir, "26082310.log"), ""+
		"00:00.000000-100,CALL,1,Usr=keep,process=rphost,OSThread=7,CallID=call-1\n"+
		"00:00.000001-100,Context,1,Usr=keep,process=rphost,OSThread=7,CallID=call-1,Context=Module.Run\n"+
		"00:00.000002-100,SDBL,1,Usr=keep,process=rphost,OSThread=7,CallID=call-1,Sdbl='SELECT * FROM Kept WHERE id=1'\n"+
		"00:00.000003-100,TLOCK,1,Usr=keep,Context=Module.Run,Locks='Table=Kept; Region=Main'\n"+
		"00:00.000004-10,SDBL,1,Usr=keep,Sdbl='SELECT * FROM TooShort WHERE id=2'\n"+
		"01:00.000000-1000,CALL,1,Usr=drop,process=rphost,OSThread=8,CallID=call-2\n"+
		"01:00.000001-1000,Context,1,Usr=drop,process=rphost,OSThread=8,CallID=call-2,Context=Module.Drop\n"+
		"01:00.000002-1000,SDBL,1,Usr=drop,process=rphost,OSThread=8,CallID=call-2,Sdbl='SELECT * FROM Dropped WHERE id=3'\n"+
		"01:00.000003-1000,TLOCK,1,Usr=drop,Context=Module.Drop,Locks='Table=Dropped; Region=Other'\n")

	result, err := Build(Options{
		InputRoot: dir, Glob: "*.log", BucketInterval: time.Minute, TopN: 10,
		Filters: []config.Filter{{Key: "Usr", Value: "keep"}}, MinDurationMicros: 50,
		TimeRange: config.TimeRange{TimeFrom: 10 * time.Hour, HasTimeFrom: true, TimeTo: 10*time.Hour + 30*time.Second, HasTimeTo: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Quality.EventsParsed != 9 {
		t.Fatalf("parse quality should include filtered events: %+v", result.Quality)
	}
	if result.Totals.Count != 4 || len(result.TopEvents) != 4 {
		t.Fatalf("filtered totals/top events = %+v / %+v", result.Totals, result.TopEvents)
	}
	if len(result.SQLRows) != 1 || result.SQLRows[0].NormalizedQuery != "SELECT * FROM Kept WHERE id=?" {
		t.Fatalf("filtered SQLRows = %+v", result.SQLRows)
	}
	if len(result.Traces) != 1 || len(result.Traces[0].Spans) != 3 || result.TraceQuality.EventsConsumed != 4 {
		t.Fatalf("filtered traces = %+v quality=%+v", result.Traces, result.TraceQuality)
	}
	if result.Locks.Quality.LockEvents != 1 || len(result.Locks.ByTable) != 1 || result.Locks.ByTable[0].Key != "Kept" {
		t.Fatalf("filtered locks = %+v", result.Locks)
	}
}

func TestBuildValidatesOptions(t *testing.T) {
	for _, options := range []Options{
		{},
		{InputRoot: "input", BucketInterval: time.Minute},
		{InputRoot: "input", Glob: "*.log"},
		{InputRoot: "input", Glob: "*.log", BucketInterval: time.Minute, TopN: -1},
		{InputRoot: "input", Glob: "*.log", BucketInterval: time.Minute, Workers: -1},
		{InputRoot: "input", Glob: "*.log", BucketInterval: time.Minute, MaxTraces: -1},
		{InputRoot: "input", Glob: "*.log", BucketInterval: time.Minute, MinDurationMicros: -1},
	} {
		if _, err := Build(options); err == nil {
			t.Fatalf("Build(%+v) error = nil", options)
		}
	}
}

func writeLog(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func eventNames(result OverviewResult) []string {
	values := make([]string, len(result.EventTypes))
	for i, row := range result.EventTypes {
		values[i] = row.Event
	}
	return values
}

func dimensionNames(rows []DimensionStat) []string {
	values := make([]string, len(rows))
	for i, row := range rows {
		values[i] = row.Value
	}
	return values
}

func rawDurations(result OverviewResult) []int64 {
	values := make([]int64, len(result.TopEvents))
	for i, row := range result.TopEvents {
		values[i] = row.DurationMicros
	}
	return values
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInt64(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
