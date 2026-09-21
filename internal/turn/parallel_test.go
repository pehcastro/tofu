package turn

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
)

const traceWait = 2 * time.Second

type callLog struct {
	mu     sync.Mutex
	events []string
	live   int
	peak   int
}

func (l *callLog) note(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *callLog) started(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, "start:"+id)
	l.live++
	l.peak = max(l.peak, l.live)
}

func (l *callLog) finished(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, "end:"+id)
	l.live--
}

func (l *callLog) at(event string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Index(l.events, event)
}

func (l *callLog) highWater() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peak
}

func (l *callLog) story() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.events, " ")
}

type barrier struct {
	mu      sync.Mutex
	need    int
	arrived int
	open    chan struct{}
}

func newBarrier(need int) *barrier {
	return &barrier{need: need, open: make(chan struct{})}
}

func (b *barrier) wait(string) error {
	b.mu.Lock()
	b.arrived++
	if b.arrived == b.need {
		close(b.open)
	}
	b.mu.Unlock()
	select {
	case <-b.open:
		return nil
	case <-time.After(traceWait):
		b.mu.Lock()
		defer b.mu.Unlock()
		return errors.New("only " + strconv.Itoa(b.arrived) + " of " + strconv.Itoa(b.need) +
			" calls had started after " + traceWait.String())
	}
}

type tracedTool struct {
	name string
	log  *callLog
	hold func(id string) error
}

func (t *tracedTool) Name() string { return t.name }

func (t *tracedTool) Definition() llm.Tool {
	return llm.Tool{Name: t.name, Description: "a traced tool", Parameters: map[string]any{"type": "object"}}
}

type tracedArgs struct {
	ID string `json:"id"`
}

func (t *tracedTool) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args tracedArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	t.log.started(args.ID)
	var held error
	if t.hold != nil {
		held = t.hold(args.ID)
	}
	t.log.finished(args.ID)
	if held != nil {
		return Result{}, held
	}
	return Result{Content: "content of " + args.ID}, nil
}

func callsTo(tool string, ids ...string) llm.Decision {
	calls := make([]llm.ToolCall, len(ids))
	for i, id := range ids {
		calls[i] = llm.ToolCall{ID: "c" + id, Name: tool, Arguments: json.RawMessage(`{"id":"` + id + `"}`)}
	}
	return toolCallDecision(calls...)
}

func parallelConfig(t *testing.T, tools []Tool, decisions ...llm.Decision) Config {
	return Config{
		Model:          &stubModel{decisions: append(decisions, messageDecision())},
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(tools...),
		Task:           "read what the model asked for",
		Caps:           Caps{MaxSteps: 10},
		ResultBytesCap: 4096,
		ArtifactDir:    t.TempDir(),
		NoCompaction:   true,
		NoFork:         true,
	}
}

func idsOf(t *testing.T, calls []ToolCallRow) []string {
	t.Helper()
	ids := make([]string, len(calls))
	for i, call := range calls {
		var args tracedArgs
		if err := json.Unmarshal(call.Args, &args); err != nil {
			t.Fatalf("tool call %d carries arguments that are not the ones the test sent: %v", i, err)
		}
		ids[i] = args.ID
	}
	return ids
}

func answersIn(row Row) []string {
	var answers []string
	for _, message := range row.Conversation {
		if message.Role == llm.RoleTool {
			answers = append(answers, message.Content)
		}
	}
	return answers
}

func TestFourReadsInOneStepRunAtOnce(t *testing.T) {
	log := &callLog{}
	together := newBarrier(4)
	config := parallelConfig(t, []Tool{&tracedTool{name: "read", log: log, hold: together.wait}},
		callsTo("read", "r1", "r2", "r3", "r4"))

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	for _, call := range row.Steps[0].ToolCalls {
		if call.Error != "" {
			t.Fatalf("a read did not see the others running: %s", call.Error)
		}
	}
	if peak := log.highWater(); peak != 4 {
		t.Fatalf("at most %d of the four reads ever ran at once, and the story is %s", peak, log.story())
	}
}

func TestFourReadsCostAboutOneWaitAndFourWritesCostFour(t *testing.T) {
	const eachCallTakes = 100 * time.Millisecond
	sleeping := func(string) error { time.Sleep(eachCallTakes); return nil }

	reads := parallelConfig(t, []Tool{&tracedTool{name: "read", log: &callLog{}, hold: sleeping}},
		callsTo("read", "r1", "r2", "r3", "r4"))
	started := time.Now()
	if _, err := Run(context.Background(), reads); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	inParallel := time.Since(started)

	writes := parallelConfig(t, []Tool{&tracedTool{name: "write", log: &callLog{}, hold: sleeping}},
		callsTo("write", "w1", "w2", "w3", "w4"))
	started = time.Now()
	if _, err := Run(context.Background(), writes); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	oneAtATime := time.Since(started)

	t.Logf("four reads took %s and four writes took %s, each call sleeping %s", inParallel, oneAtATime, eachCallTakes)
	if inParallel > 3*eachCallTakes {
		t.Fatalf("four reads took %s, which is more than one wait of %s", inParallel, eachCallTakes)
	}
	if oneAtATime < 4*eachCallTakes {
		t.Fatalf("four writes took %s, so they did not run one at a time", oneAtATime)
	}
}

func TestAWriteStopsTheParallelPrefixWhereItSits(t *testing.T) {
	log := &callLog{}
	together := newBarrier(2)
	writing := func(string) error {
		if log.at("start:r3") >= 0 {
			return errors.New("the read after the write started while the write was still running")
		}
		return nil
	}
	config := parallelConfig(t,
		[]Tool{
			&tracedTool{name: "read", log: log, hold: together.wait},
			&tracedTool{name: "write", log: log, hold: writing},
		},
		toolCallDecision(
			llm.ToolCall{ID: "cr1", Name: "read", Arguments: json.RawMessage(`{"id":"r1"}`)},
			llm.ToolCall{ID: "cr2", Name: "read", Arguments: json.RawMessage(`{"id":"r2"}`)},
			llm.ToolCall{ID: "cw", Name: "write", Arguments: json.RawMessage(`{"id":"w"}`)},
			llm.ToolCall{ID: "cr3", Name: "read", Arguments: json.RawMessage(`{"id":"r3"}`)},
		))

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	for _, call := range row.Steps[0].ToolCalls {
		if call.Error != "" {
			t.Fatalf("%s failed: %s, and the story is %s", call.Tool, call.Error, log.story())
		}
	}
	if log.at("start:r2") > log.at("end:r1") {
		t.Fatalf("the two reads before the write did not run together: %s", log.story())
	}
	if log.at("end:w") > log.at("start:r3") {
		t.Fatalf("the read after the write did not wait for it: %s", log.story())
	}
}

func TestResultsReachTheModelInTheOrderTheCallsWereMade(t *testing.T) {
	log := &callLog{}
	together := newBarrier(4)
	linger := map[string]time.Duration{
		"r1": 120 * time.Millisecond,
		"r2": 80 * time.Millisecond,
		"r3": 40 * time.Millisecond,
	}
	config := parallelConfig(t, []Tool{&tracedTool{name: "read", log: log, hold: func(id string) error {
		if err := together.wait(id); err != nil {
			return err
		}
		time.Sleep(linger[id])
		return nil
	}}}, callsTo("read", "r1", "r2", "r3", "r4"))

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if log.at("end:r4") > log.at("end:r1") {
		t.Fatalf("the calls did not finish in reverse, so this proves nothing: %s", log.story())
	}
	asked := []string{"r1", "r2", "r3", "r4"}
	if got := idsOf(t, row.Steps[0].ToolCalls); !slices.Equal(got, asked) {
		t.Fatalf("the recorded step lists the calls as %v, and the model made them as %v", got, asked)
	}
	want := []string{"content of r1", "content of r2", "content of r3", "content of r4"}
	if got := answersIn(row); !slices.Equal(got, want) {
		t.Fatalf("the model was answered %v, and it asked in the order %v", got, want)
	}
}

type tracingGate struct {
	log     *callLog
	denying string
}

func (g tracingGate) Decide(_ context.Context, request GateRequest) (GateDecision, error) {
	var args tracedArgs
	if err := json.Unmarshal(request.Args, &args); err != nil {
		return GateDecision{}, err
	}
	g.log.note("gate:" + args.ID)
	verdict := ledger.VerdictAllow
	if args.ID == g.denying {
		verdict = ledger.VerdictDeny
	}
	return GateDecision{ID: "d-" + args.ID, Verdict: verdict}, nil
}

func TestEveryCallIsGatedBeforeAnyCallRuns(t *testing.T) {
	log := &callLog{}
	config := parallelConfig(t, []Tool{&tracedTool{name: "read", log: log}},
		callsTo("read", "r1", "r2", "r3", "r4"))
	config.Gate = tracingGate{log: log}

	if _, err := Run(context.Background(), config); err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if log.at("gate:r4") > log.at("start:r1") {
		t.Fatalf("a call ran before the last verdict of its batch: %s", log.story())
	}
}

func TestADenyInsideAPrefixRefusesThatCallAndNotTheOthers(t *testing.T) {
	log := &callLog{}
	together := newBarrier(3)
	config := parallelConfig(t, []Tool{&tracedTool{name: "read", log: log, hold: together.wait}},
		callsTo("read", "r1", "r2", "r3", "r4"))
	config.Gate = tracingGate{log: log, denying: "r3"}
	config.GateMode = GateEnforce

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	calls := row.Steps[0].ToolCalls
	if got := idsOf(t, calls); !slices.Equal(got, []string{"r1", "r2", "r3", "r4"}) {
		t.Fatalf("the step lists the calls as %v", got)
	}
	if !strings.Contains(calls[2].Error, "the verdict is deny") {
		t.Fatalf("the denied call reads %+v", calls[2])
	}
	if log.at("start:r3") >= 0 {
		t.Fatalf("the denied call ran anyway: %s", log.story())
	}
	for _, i := range []int{0, 1, 3} {
		if calls[i].Error != "" {
			t.Fatalf("the call beside the deny was refused too: %+v, and the story is %s", calls[i], log.story())
		}
	}
}

func TestTheRecordedStepSaysWhichCallsRanTogether(t *testing.T) {
	log := &callLog{}
	together := newBarrier(konst.TurnParallelToolCalls)
	ids := make([]string, konst.TurnParallelToolCalls)
	calls := make([]llm.ToolCall, 0, len(ids)+1)
	for i := range ids {
		ids[i] = "r" + strconv.Itoa(i+1)
		calls = append(calls, llm.ToolCall{ID: "c" + ids[i], Name: "read", Arguments: json.RawMessage(`{"id":"` + ids[i] + `"}`)})
	}
	calls = append(calls, llm.ToolCall{ID: "cw", Name: "write", Arguments: json.RawMessage(`{"id":"w"}`)})
	config := parallelConfig(t,
		[]Tool{
			&tracedTool{name: "read", log: log, hold: together.wait},
			&tracedTool{name: "write", log: log},
		},
		toolCallDecision(calls...))

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	_, events, err := row.Record()
	if err != nil {
		t.Fatalf("the turn did not record: %v", err)
	}
	var recorded StepRow
	for _, event := range events {
		if event.Kind != session.EventStep {
			continue
		}
		if err := json.Unmarshal(event.Body, &recorded); err != nil {
			t.Fatalf("the recorded step does not read back: %v", err)
		}
		if named := strings.Count(string(event.Body), "parallel_batch"); named != len(ids) {
			t.Fatalf("%d calls in the record name a batch, and %d ran together: %s", named, len(ids), event.Body)
		}
		break
	}
	for i := range ids {
		if recorded.ToolCalls[i].ParallelBatch != 1 {
			t.Fatalf("read %s is recorded in batch %d, and all the reads ran as one: %+v",
				ids[i], recorded.ToolCalls[i].ParallelBatch, recorded.ToolCalls)
		}
	}
	if write := recorded.ToolCalls[len(ids)]; write.ParallelBatch != 0 {
		t.Fatalf("the write ran alone and is recorded in batch %d", write.ParallelBatch)
	}
}

func TestMoreCallsThanTheCapRunInWaves(t *testing.T) {
	log := &callLog{}
	overlapping := func(string) error { time.Sleep(60 * time.Millisecond); return nil }
	ids := make([]string, konst.TurnParallelToolCalls+2)
	for i := range ids {
		ids[i] = "r" + strconv.Itoa(i+1)
	}
	config := parallelConfig(t, []Tool{&tracedTool{name: "read", log: log, hold: overlapping}},
		callsTo("read", ids...))

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if peak := log.highWater(); peak != konst.TurnParallelToolCalls {
		t.Fatalf("%d calls ran at once and the cap is %d: %s", peak, konst.TurnParallelToolCalls, log.story())
	}
	if got := idsOf(t, row.Steps[0].ToolCalls); !slices.Equal(got, ids) {
		t.Fatalf("the step lists the calls as %v, and the model made them as %v", got, ids)
	}
}

func TestFetchAndWebSearchRunInTheSameWaveAsARead(t *testing.T) {
	log := &callLog{}
	together := newBarrier(3)
	config := parallelConfig(t,
		[]Tool{
			&tracedTool{name: "read", log: log, hold: together.wait},
			&tracedTool{name: "fetch", log: log, hold: together.wait},
			&tracedTool{name: "web_search", log: log, hold: together.wait},
		},
		toolCallDecision(
			llm.ToolCall{ID: "cs", Name: "web_search", Arguments: json.RawMessage(`{"id":"s1"}`)},
			llm.ToolCall{ID: "cf", Name: "fetch", Arguments: json.RawMessage(`{"id":"f1"}`)},
			llm.ToolCall{ID: "cr", Name: "read", Arguments: json.RawMessage(`{"id":"r1"}`)},
		))

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	for _, call := range row.Steps[0].ToolCalls {
		if call.Error != "" {
			t.Fatalf("%s did not see the others running: %s", call.Tool, call.Error)
		}
		if call.ParallelBatch != 1 {
			t.Fatalf("%s is in batch %d, and the three read-only calls are one wave: %+v", call.Tool, call.ParallelBatch, row.Steps[0].ToolCalls)
		}
	}
	if peak := log.highWater(); peak != 3 {
		t.Fatalf("at most %d of the three ran at once, and the story is %s", peak, log.story())
	}
}
