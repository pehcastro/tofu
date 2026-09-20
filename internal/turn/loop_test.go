package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
)

func baseConfig(t *testing.T, model Model, tools Registry) Config {
	t.Helper()
	scratch := session.NewStore(t.TempDir())
	return Config{
		Model:          model,
		Spend:          SpendAPIKey,
		Tools:          tools,
		Task:           "say pong",
		Wire:           "anthropic",
		Caps:           Caps{MaxSteps: 10},
		ResultBytesCap: 4096,
		EndedSession:   func(row Row) error { return WriteSession(scratch, row) },
	}
}

func TestRunDrivesTheLoopToCompletionAndRecordsEveryStepAndCall(t *testing.T) {
	tool := &stubTool{name: "read", result: Result{Content: "file contents", Command: "read a.txt"}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry(tool)))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	if row.Outcome != OutcomeStopped {
		t.Fatalf("expected outcome stopped, got %s", row.Outcome)
	}
	if len(row.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(row.Steps))
	}
	if len(row.Steps[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call on step 1, got %d", len(row.Steps[0].ToolCalls))
	}
	call := row.Steps[0].ToolCalls[0]
	if call.Tool != "read" || call.Command != "read a.txt" {
		t.Fatalf("tool call row is %+v", call)
	}
	if row.Steps[1].AssistantText != "done" {
		t.Fatalf("expected the final step to carry the assistant's text, got %+v", row.Steps[1])
	}
	if tool.calls != 1 {
		t.Fatalf("expected the tool to run once, ran %d times", tool.calls)
	}
}

func TestRunRefusesAMalformedToolCallAtTheBoundaryAndContinues(t *testing.T) {
	tool, err := NewReadTool(t.TempDir())
	if err != nil {
		t.Fatalf("building the read tool: %v", err)
	}

	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry(tool)))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if len(row.Steps) != 2 {
		t.Fatalf("expected the turn to continue past the malformed call, got %d steps", len(row.Steps))
	}
	call := row.Steps[0].ToolCalls[0]
	if call.Error == "" {
		t.Fatalf("expected the tool call row to carry an error, got %+v", call)
	}
	if !strings.Contains(call.Error, "path is required") {
		t.Fatalf("error did not name what was wrong: %q", call.Error)
	}
	if row.Outcome != OutcomeStopped {
		t.Fatalf("expected the turn to still reach stopped, got %s", row.Outcome)
	}
}

func TestRunRefusesAnUnknownTool(t *testing.T) {
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "delete_everything", Arguments: json.RawMessage(`{}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry()))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	call := row.Steps[0].ToolCalls[0]
	if !strings.Contains(call.Error, "unknown tool") {
		t.Fatalf("expected an unknown tool error, got %q", call.Error)
	}
}

func TestRunRecordsACommandsExitCodeAndCommandString(t *testing.T) {
	code := 3
	tool := &stubTool{name: "bash", result: Result{Content: "did the thing", Command: "exit 3", ExitCode: &code}}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"exit 3"}`)}),
		messageDecision(),
	}}

	row, err := Run(context.Background(), baseConfig(t, model, NewRegistry(tool)))
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	call := row.Steps[0].ToolCalls[0]
	if call.Command != "exit 3" {
		t.Fatalf("expected the command string to be recorded, got %q", call.Command)
	}
	if call.ExitCode == nil || *call.ExitCode != 3 {
		t.Fatalf("expected exit code 3 recorded, got %+v", call.ExitCode)
	}
}

type measuringModel struct {
	decisions []llm.Decision
	calls     int
	mu        sync.Mutex
	awoken    chan struct{}
	asks      int64
	contexts  []int
	requests  [][]llm.Message
}

func (m *measuringModel) asksAndTheNextWakeUp() (int64, <-chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.awoken == nil {
		m.awoken = make(chan struct{})
	}
	return m.asks, m.awoken
}

func (m *measuringModel) waitForAnAskAfter(asked int64, giveUp <-chan time.Time) bool {
	for {
		asks, awoken := m.asksAndTheNextWakeUp()
		if asks > asked {
			return true
		}
		select {
		case <-awoken:
		case <-giveUp:
			return false
		}
	}
}

func (m *measuringModel) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	m.mu.Lock()
	m.asks++
	if m.awoken != nil {
		close(m.awoken)
		m.awoken = nil
	}
	m.mu.Unlock()
	carried := 0
	for _, message := range request.Messages {
		carried += len(message.Content)
		for _, call := range message.ToolCalls {
			carried += len(call.Name) + len(call.Arguments)
		}
	}
	m.contexts = append(m.contexts, carried)
	m.requests = append(m.requests, slices.Clone(request.Messages))
	if m.calls >= len(m.decisions) {
		return llm.Decision{}, errors.New("measuringModel: no more decisions queued")
	}
	m.calls++
	return m.decisions[m.calls-1], nil
}

const (
	longTurnSteps       = 10
	longTurnResultBytes = 20000
	noDeadlineForkWait  = time.Minute
)

func compactionsIn(row Row) []Compaction {
	var events []Compaction
	for _, step := range row.Steps {
		if step.Compaction != nil {
			events = append(events, *step.Compaction)
		}
	}
	return events
}

func longTurnConfig(t *testing.T) (Config, *measuringModel) {
	t.Helper()
	var decisions []llm.Decision
	for i := 1; i <= longTurnSteps; i++ {
		decisions = append(decisions, toolCallDecision(llm.ToolCall{
			ID:        "call-" + strconv.Itoa(i),
			Name:      "read",
			Arguments: json.RawMessage(`{"path":"src/file` + strconv.Itoa(i%3) + `.ts"}`),
		}))
	}
	model := &measuringModel{decisions: append(decisions, messageDecision())}
	body := strings.Repeat("x", longTurnResultBytes)
	config := baseConfig(t, model, NewRegistry(&stubTool{name: "read", result: Result{Content: body, Command: "read"}}))
	config.Caps = Caps{MaxSteps: longTurnSteps + 2}
	config.ResultBytesCap = longTurnResultBytes * 2
	config.ArtifactDir = t.TempDir()
	config.Budget = recall.Budget{
		Model:         "a stub with the shipped window",
		CeilingTokens: konst.ContextCeilingTokens,
		Bands:         recall.ShippedBands(),
		Automatic:     true,
		Source:        "the shipped ceiling this build measures against",
	}
	return config, model
}

func longTurn(t *testing.T, noCompaction bool) (*measuringModel, Row, string) {
	t.Helper()
	config, model := longTurnConfig(t)
	config.NoCompaction = noCompaction
	config.NoFork = true

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return model, row, config.ArtifactDir
}

func forkingTurn(t *testing.T) (*measuringModel, Row, []Row, string) {
	t.Helper()
	config, model := longTurnConfig(t)
	sessions := make(chan Row, longTurnSteps)
	config.EndedSession = func(session Row) error {
		sessions <- session
		return nil
	}

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	close(sessions)
	var ended []Row
	for session := range sessions {
		ended = append(ended, session)
	}
	return model, row, ended, config.ArtifactDir
}

func TestATurnThatCrossesTheTargetForksAndTheEndedSessionIsKeptWhole(t *testing.T) {
	model, row, ended, _ := forkingTurn(t)
	if len(ended) == 0 {
		t.Fatal("a turn carrying more than the band target never forked")
	}
	first := ended[0]
	if first.Outcome != OutcomeForked || first.ForkedInto == "" {
		t.Fatalf("the session that ended says outcome %s and forked_into %q", first.Outcome, first.ForkedInto)
	}
	if row.ForkedFrom != ended[len(ended)-1].ID {
		t.Fatalf("the session that began says it came from %q and the last ended session is %q", row.ForkedFrom, ended[len(ended)-1].ID)
	}
	fork := first.Steps[len(first.Steps)-1].Fork
	if fork == nil {
		t.Fatal("the step the fork happened on carries no fork record")
	}
	if fork.TokensAfter >= fork.TokensBefore {
		t.Fatalf("the session that began carries %d tokens against %d in the one that ended", fork.TokensAfter, fork.TokensBefore)
	}
	record, err := json.Marshal(first.Steps[len(first.Steps)-1])
	if err != nil {
		t.Fatalf("marshal step row: %v", err)
	}
	t.Logf("ended %s forked_into %s at step %d, %d tokens to %d, blocked %d microseconds\n%s",
		first.ID, first.ForkedInto, fork.Step, fork.TokensBefore, fork.TokensAfter, fork.BlockedMicros, record)

	body := strings.Repeat("x", longTurnResultBytes)
	for _, step := range first.Steps {
		if step.Compaction != nil {
			t.Fatalf("step %d of the session that ended was rewritten in place: %+v", step.Index, step.Compaction)
		}
		for _, call := range step.ToolCalls {
			if call.ResultBytes != len(body) {
				t.Fatalf("step %d records %d result bytes where the tool returned %d", step.Index, call.ResultBytes, len(body))
			}
		}
	}
	if model.contexts[len(model.contexts)-1] >= model.contexts[fork.Step-1] {
		t.Fatalf("the step after the fork carried %d bytes against %d at the fork",
			model.contexts[len(model.contexts)-1], model.contexts[fork.Step-1])
	}
}

func TestEveryResultOfTheEndedSessionIsReadableFromTheNewOne(t *testing.T) {
	_, row, ended, dir := forkingTurn(t)
	if len(ended) == 0 {
		t.Fatal("the turn never forked")
	}

	store := recall.NewStore(dir)
	body := strings.Repeat("x", longTurnResultBytes)
	carried := 0
	for _, session := range ended {
		fork := session.Steps[len(session.Steps)-1].Fork
		for _, result := range fork.Carry.Results {
			back, err := store.Fetch(result.Handle)
			if err != nil {
				t.Fatalf("fetch %s: %v", result.Handle, err)
			}
			if !strings.Contains(string(back), body) {
				t.Fatalf("artifact %s gave back %d bytes and none of them are the result it names", result.Handle, len(back))
			}
			carried++
		}
	}
	t.Logf("%d results carried by handle across %d forks into %s", carried, len(ended), row.ID)
	if carried == 0 {
		t.Fatal("the carry named no results, so the new session has no way back to the work")
	}
}

func TestTheStepAfterAForkBeginsBeforeTheForkIsWrittenOut(t *testing.T) {
	config, model := longTurnConfig(t)
	budget := noDeadlineForkWait
	if deadline, bounded := t.Deadline(); bounded {
		budget = time.Until(deadline) / 2
	}
	var forks, unanswered, askedAtLastUnanswered atomic.Int64
	config.EndedSession = func(ended Row) error {
		forks.Add(1)
		asksAtTheFork := int64(len(ended.Steps))
		giveUp := time.NewTimer(budget)
		defer giveUp.Stop()
		if !model.waitForAnAskAfter(asksAtTheFork, giveUp.C) {
			unanswered.Add(1)
			askedAtLastUnanswered.Store(asksAtTheFork)
		}
		return nil
	}

	if _, err := Run(context.Background(), config); err != nil {
		t.Fatalf("run: %v", err)
	}
	if forks.Load() == 0 {
		t.Fatal("the turn never forked, so nothing was proved about waiting")
	}
	if unanswered.Load() > 0 {
		asks, _ := model.asksAndTheNextWakeUp()
		if asks > askedAtLastUnanswered.Load() {
			t.Fatalf("%d of %d forks waited %s before the model was asked again, so writing the ended session sits in front of the next step",
				unanswered.Load(), forks.Load(), budget)
		}
		t.Fatalf("%d of %d forks waited %s and the model was never asked again: the fork fell on the last step of the turn, so this test proves nothing about what waits for what",
			unanswered.Load(), forks.Load(), budget)
	}
	t.Logf("each of %d forks was still being written when the next step asked the model, and the run finished without waiting for any of them",
		forks.Load())
}

func TestALongTurnCompactsAndSaysWhatItDroppedAndEveryDropComesBackWhole(t *testing.T) {
	model, row, dir := longTurn(t, false)
	events := compactionsIn(row)
	if len(events) == 0 {
		t.Fatal("a turn carrying more than the band target compacted nothing")
	}
	for _, step := range row.Steps {
		if step.Compaction == nil {
			continue
		}
		record, err := json.Marshal(step)
		if err != nil {
			t.Fatalf("marshal step row: %v", err)
		}
		t.Logf("step row: %s", record)
	}
	t.Logf("context carried at the last step: %d bytes", model.contexts[len(model.contexts)-1])

	store := recall.NewStore(dir)
	body := strings.Repeat("x", longTurnResultBytes)
	for _, event := range events {
		for _, drop := range event.Drops {
			back, err := store.Fetch(drop.Handle)
			if err != nil {
				t.Fatalf("fetch %s: %v", drop.Handle, err)
			}
			if string(back) != body {
				t.Fatalf("handle %s gave back %d bytes where %d went in", drop.Handle, len(back), len(body))
			}
			if drop.Bytes != len(body) {
				t.Fatalf("the row says %d bytes were dropped and the result was %d", drop.Bytes, len(body))
			}
		}
	}
}

func TestTheSameTurnWithCompactionOffCarriesMoreAtTheLastStep(t *testing.T) {
	compacted, onRow, _ := longTurn(t, false)
	whole, offRow, _ := longTurn(t, true)
	events := compactionsIn(onRow)
	if none := compactionsIn(offRow); len(none) != 0 {
		t.Fatalf("compaction was off and %d compactions happened anyway", len(none))
	}
	if len(events) == 0 {
		t.Fatal("compaction was on and nothing happened, so there is nothing to compare")
	}
	last := len(whole.contexts) - 1
	t.Logf("last step carried %d bytes with compaction off and %d with it on, %d compactions",
		whole.contexts[last], compacted.contexts[last], len(events))
	if compacted.contexts[last] >= whole.contexts[last] {
		t.Fatalf("compaction carried %d bytes at the last step against %d without it",
			compacted.contexts[last], whole.contexts[last])
	}
}

func TestRunSurfacesAModelError(t *testing.T) {
	sentinel := &errorModel{err: context.DeadlineExceeded}
	_, err := Run(context.Background(), baseConfig(t, sentinel, NewRegistry()))
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestAForkedTurnIsWrittenAsAHeaderAndAJSONLBodyThatNameTheLineage(t *testing.T) {
	config, _ := longTurnConfig(t)
	dir := t.TempDir()
	scratch := session.NewStore(dir)
	config.System = "the rules this turn works under, which are the identity band and nothing else"
	config.EndedSession = func(row Row) error { return WriteSession(scratch, row) }

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := WriteSession(scratch, row); err != nil {
		t.Fatalf("write the session the turn ended in: %v", err)
	}

	lineage, err := scratch.Lineage(row.ID)
	if err != nil {
		t.Fatalf("lineage of %s: %v", row.ID, err)
	}
	if len(lineage) < 2 {
		t.Fatalf("the turn wrote a lineage of %d, so it never forked and nothing is proved", len(lineage))
	}
	for i, header := range lineage {
		if header.Root != lineage[0].ID {
			t.Errorf("%s names root %q, want %s", header.ID, header.Root, lineage[0].ID)
		}
		if i > 0 && header.Parent != lineage[i-1].ID {
			t.Errorf("%s names parent %q, want %s", header.ID, header.Parent, lineage[i-1].ID)
		}
		if i > 0 && header.ForkKind != string(ForkContinuation) {
			t.Errorf("%s names fork kind %q", header.ID, header.ForkKind)
		}
		if header.Wire != "anthropic" || header.Task != config.Task {
			t.Errorf("%s names wire %q and task %q", header.ID, header.Wire, header.Task)
		}
	}
	ended := lineage[0]
	if ended.ForkTokensBefore <= ended.ForkTokensAfter || ended.ForkTokensAfter == 0 {
		t.Errorf("the header of %s carries %d tokens before the fork and %d after",
			ended.ID, ended.ForkTokensBefore, ended.ForkTokensAfter)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ended.ID, "body.jsonl"))
	if err != nil {
		t.Fatalf("read the body of %s: %v", ended.ID, err)
	}
	events, err := scratch.Body(ended.ID)
	if err != nil {
		t.Fatalf("read the events of %s: %v", ended.ID, err)
	}
	if strings.Count(string(raw), "\n") != len(events) {
		t.Fatalf("the body holds %d line endings and %d events, so it is not one event per line",
			strings.Count(string(raw), "\n"), len(events))
	}
	if events[len(events)-1].Kind != session.EventOutcome {
		t.Fatalf("the last event of %s is %q, want the outcome", ended.ID, events[len(events)-1].Kind)
	}

	last := -1
	for index, event := range events {
		if event.Kind == session.EventStep {
			last = index
		}
	}
	if last < 0 {
		t.Fatal("the ended session recorded no step at all")
	}
	var forked StepRow
	if err := json.Unmarshal(events[last].Body, &forked); err != nil {
		t.Fatalf("the step the fork happened on does not read back: %v", err)
	}
	if forked.Fork == nil {
		t.Fatal("the last step of the ended session carries no fork record")
	}
	if forked.Occupancy == nil {
		t.Fatal("the step that forked recorded no occupancy, so the number the fork decided on is not in the record")
	}
	bands := *forked.Occupancy
	if bands.Identity == 0 || bands.WorkingSet == 0 || bands.Recent == 0 || bands.Target == 0 {
		t.Fatalf("the occupancy reads %+v, and this turn put something in every band but facts", bands)
	}
	if bands.Total() != forked.Fork.TokensBefore {
		t.Fatalf("the bands add to %d and the fork says %d tokens were carried", bands.Total(), forked.Fork.TokensBefore)
	}
	t.Logf("%d sessions, root %s, head %s; the fork step carries identity %d facts %d working set %d recent %d against target %d",
		len(lineage), ended.Root, row.ID, bands.Identity, bands.Facts, bands.WorkingSet, bands.Recent, bands.Target)
}

func TestPackageMakesNoNetworkCallOfItsOwn(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing package files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no source files found, the test found nothing")
	}
	forbidden := []string{`"net/http"`, `"net"`}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		for _, imp := range forbidden {
			if strings.Contains(string(data), imp) {
				t.Fatalf("%s imports %s: the turn loop must call the model only through the Model interface", file, imp)
			}
		}
	}
}
