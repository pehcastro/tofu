package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/models"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/turn"
)

func TestARealRunOffersSymbolsAndTheOffArmDoesNot(t *testing.T) {
	full := toolNames(t, armOpts(t))
	if !slices.Contains(full, "symbols") {
		t.Fatalf("a real run cannot ask where an identifier is declared: %v", full)
	}
	if three := toolNames(t, armOpts(t, "--tools", toolSetThree)); slices.Contains(three, "symbols") {
		t.Fatalf("the off arm must stay the three tools the recorded runs had: %v", three)
	}
}

type deletingModel struct {
	decisions []llm.Decision
	deletes   string
	asked     int
}

func (m *deletingModel) Ask(context.Context, llm.Request) (llm.Decision, error) {
	if m.asked == 1 {
		if err := os.Remove(m.deletes); err != nil {
			return llm.Decision{}, err
		}
	}
	m.asked++
	return m.decisions[m.asked-1], nil
}

func TestASecondIdenticalReadInOneTurnNeverReachesTheTool(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	readable := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(readable, []byte("the note as it was written once"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := armOpts(t)
	opts.dir, opts.task = dir, "read the note twice"
	built, _, err := buildRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	read := func(id string) llm.Decision {
		return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"note.txt"}`)},
		}}
	}
	model := &deletingModel{deletes: readable, decisions: []llm.Decision{
		read("call-1"),
		read("call-2"),
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "read it twice"},
	}}

	config, _ := runConfig(opts, built, runtime{model: model, spend: turn.SpendSubscription})
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	if len(row.Steps) < 2 || len(row.Steps[1].ToolCalls) != 1 {
		t.Fatalf("the turn ran %d steps, want two reads and an answer", len(row.Steps))
	}
	second := row.Steps[1].ToolCalls[0]
	if second.Error != "" {
		t.Fatalf("the second read reached the tool and the file was gone by then: %s", second.Error)
	}
	var answers []string
	for _, message := range row.Conversation {
		if message.Role == llm.RoleTool {
			answers = append(answers, message.Content)
		}
	}
	if len(answers) != 2 || !strings.HasPrefix(answers[1], "cached: ") {
		t.Fatalf("the second answer does not say it came from the first: %q", answers)
	}
	if !strings.HasSuffix(answers[1], answers[0]) {
		t.Fatalf("the cached answer carries something other than what the first read returned: %q", answers[1])
	}
	t.Logf("the file was deleted between the two reads and the second answered %d bytes: %q", second.ResultBytes, answers[1])
}

func ranTool(t *testing.T, dir, name, args string) turn.Result {
	t.Helper()
	built, _, err := buildRunTools(dir, toolSetFull)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	for _, tool := range built {
		if tool.Name() != name {
			continue
		}
		result, err := tool.Run(context.Background(), json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s %s: %v", name, args, err)
		}
		return result
	}
	t.Fatalf("no tool named %s was built", name)
	return turn.Result{}
}

func TestABashResultCarryingAFabricatedCitationReachesTheModelRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.go"), []byte("package real\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := ranTool(t, dir, "bash", `{"command":"echo the fix is in real.go:900 and in missing.go:1"}`)
	if !strings.Contains(result.Content, "citations: 2 found, 0 resolved, 2 refused") {
		t.Fatalf("a bash result carrying two made up citations reached the model unchecked:\n%s", result.Content)
	}
	for _, want := range []string{"refused real.go:900", "refused missing.go:1"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("the verdict does not name %q:\n%s", want, result.Content)
		}
	}
	t.Logf("%s", result.Content)
}

func TestASearchResultGetsNoCitationVerdictBecauseThisBinaryWroteIt(t *testing.T) {
	dir := t.TempDir()
	var lines strings.Builder
	for range 200 {
		lines.WriteString("wanted := 1\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "many.go"), []byte(lines.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	result := ranTool(t, dir, "search", `{"pattern":"wanted"}`)
	if !strings.Contains(result.Content, "many.go:1-200") {
		t.Fatalf("search did not frame the 200 written lines as one unit, so the count proves nothing:\n%s", result.Content)
	}
	if strings.Contains(result.Content, "citations:") {
		t.Fatalf("a search result carries a citation verdict on output this binary generated:\n%s", result.Content)
	}
	t.Logf("search returned %d bytes over 200 matches and no citation verdict", len(result.Content))
}

func TestTheContextCeilingFlagWinsOverTheEnvironmentVariable(t *testing.T) {
	t.Setenv(recall.CeilingVariable, "90000")
	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--context-ceiling", "20000", "a task"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	budget, err := contextBudget(opts, chosenFor(t))
	if err != nil {
		t.Fatalf("contextBudget: %v", err)
	}
	if budget.CeilingTokens != 20000 || !budget.Automatic {
		t.Fatalf("the flag did not set the ceiling: %+v", budget)
	}
	if !strings.Contains(budget.Source, recall.CeilingVariable) {
		t.Fatalf("the record does not say which of the two won: %q", budget.Source)
	}

	variableAlone, err := contextBudget(armOpts(t), chosenFor(t))
	if err != nil {
		t.Fatalf("contextBudget with the variable alone: %v", err)
	}
	if variableAlone.CeilingTokens != 90000 {
		t.Fatalf("the variable stopped working when the flag arrived: %+v", variableAlone)
	}
	t.Logf("flag: %s\nvariable: %s", budget.Record(), variableAlone.Record())
}

func TestARunAtATwentyThousandCeilingCompactsAndTheRecordSaysWhatItDropped(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	opts, err := parseRunArgs([]string{"--dir", dir, "--context-ceiling", "20000", "--no-crew", "read the big file over and over"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat("x", 20000)), 0o600); err != nil {
		t.Fatal(err)
	}
	built, _, err := buildRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	var decisions []llm.Decision
	for i := range 8 {
		decisions = append(decisions, llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-" + string(rune('a'+i)), Name: "bash",
				Arguments: json.RawMessage(`{"command":"echo ` + strconv.Itoa(i) + " " + strings.Repeat("y", 6000) + `"}`)},
		}})
	}
	decisions = append(decisions, llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "read it"})

	budget, err := contextBudget(opts, chosenFor(t))
	if err != nil {
		t.Fatalf("contextBudget: %v", err)
	}
	config, _ := runConfig(opts, built, runtime{model: &queuedModel{decisions: decisions}, spend: turn.SpendSubscription, budget: budget})
	config.NoFork = true
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	dropped := 0
	for _, step := range row.Steps {
		if step.Compaction == nil {
			continue
		}
		for _, drop := range step.Compaction.Drops {
			dropped++
			if drop.Handle == "" || drop.Bytes == 0 || drop.Reason == "" {
				t.Fatalf("a drop says %+v, and a record of what was dropped names the tool, the bytes, the reason and the handle it is kept under", drop)
			}
			t.Logf("step %d dropped the %s result of %d bytes as %s, kept at %s",
				step.Compaction.Step, drop.Tool, drop.Bytes, drop.Reason, drop.Handle)
		}
	}
	if dropped == 0 {
		t.Fatalf("a %d token ceiling compacted nothing across %d steps", budget.CeilingTokens, len(row.Steps))
	}
}

func TestRunHelpKeepsTheCeilingFlagWorkingAndDoesNotOfferIt(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runVerb([]string{"--help"}, &out, &errOut); code != exitOK {
		t.Fatalf("tofu run --help exited %d: %s", code, errOut.String())
	}
	if strings.Contains(out.String(), "ceiling") {
		t.Fatalf("the help offers a development flag to whoever reads it:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "--dir") {
		t.Fatalf("the help does not name the one required argument:\n%s", out.String())
	}

	opts, err := parseRunArgs([]string{"--dir", t.TempDir(), "--context-ceiling", "20000", "a task"})
	if err != nil {
		t.Fatalf("the flag the help no longer names stopped working: %v", err)
	}
	budget, err := contextBudget(opts, chosenFor(t))
	if err != nil {
		t.Fatalf("contextBudget: %v", err)
	}
	if budget.CeilingTokens != 20000 {
		t.Fatalf("the flag set a %d token ceiling, want the 20000 it was given", budget.CeilingTokens)
	}
	t.Logf("%s", out.String())
}

func TestARequestOverTheModelsWindowIsRefusedBeforeItIsSent(t *testing.T) {
	budget, err := recall.BudgetFor("a model with a small window", 1000)
	if err != nil {
		t.Fatalf("budget: %v", err)
	}
	queued := &queuedModel{decisions: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "sent"}}}
	guard, err := guarded(queued, budget)
	if err != nil {
		t.Fatalf("guarded: %v", err)
	}

	_, err = guard.Ask(context.Background(), llm.Request{Messages: []llm.Message{
		{Role: llm.RoleUser, Content: strings.Repeat("x", 10000000)},
	}})
	var over *recall.OverWindow
	if !errors.As(err, &over) {
		t.Fatalf("a request far past the window came back as %v, want a typed refusal", err)
	}
	if over.WindowTokens != 1000 || over.RequestTokens <= 1000 {
		t.Fatalf("the refusal says %+v, and it has to name the window and the size of the request", over)
	}
	if len(queued.decisions) != 1 {
		t.Fatal("the model was asked anyway, so the request reached the wire")
	}

	decision, err := guard.Ask(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "small"}}})
	if err != nil || decision.Content != "sent" {
		t.Fatalf("a request inside the window was not sent: %v %+v", err, decision)
	}
	t.Logf("refused: %v", over)
}

func TestAModelWithNoRecordedWindowCompactsAtTheOperatingCeiling(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	opts, err := parseRunArgs([]string{"--dir", dir, "--no-crew", "read every part of the dump"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	var decisions []llm.Decision
	for i := range 10 {
		name := "part-" + strconv.Itoa(i) + ".txt"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("x", 20000)), 0o600); err != nil {
			t.Fatal(err)
		}
		decisions = append(decisions, llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-" + strconv.Itoa(i), Name: "bash", Arguments: json.RawMessage(`{"command":"cat ` + name + `"}`)},
		}})
	}
	decisions = append(decisions, llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "read it all"})
	built, _, err := buildRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}

	budget, err := contextBudget(opts, models.Model{ID: "a model no library records"})
	if err != nil {
		t.Fatalf("contextBudget: %v", err)
	}
	if budget.WindowTokens != 0 || budget.CeilingTokens != konst.ContextCeilingTokens {
		t.Fatalf("a model nobody has a window for got %+v, want no window and the %d token ceiling tofu operates under",
			budget, konst.ContextCeilingTokens)
	}
	store := session.NewStore(filepath.Join(dir, ".tofu", "sessions"))
	config, _ := runConfig(opts, built, runtime{model: &queuedModel{decisions: decisions}, spend: turn.SpendSubscription, budget: budget, sessions: store})
	config.NoFork = true
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}

	dropped := 0
	for _, step := range row.Steps {
		if step.Compaction != nil {
			dropped += len(step.Compaction.Drops)
		}
	}
	if dropped == 0 {
		t.Fatalf("a model with no recorded window compacted nothing across %d steps against a %d token target",
			len(row.Steps), budget.Bands.Target())
	}
	header, err := store.Header(row.ID)
	if err != nil {
		t.Fatalf("reading the record back: %v", err)
	}
	if header.ContextCeiling != konst.ContextCeilingTokens || !strings.Contains(header.AutoCompaction, "no context window is recorded") {
		t.Fatalf("the record says ceiling %d and %q", header.ContextCeiling, header.AutoCompaction)
	}
	t.Logf("%d results dropped over %d steps, and the record reads: %s", dropped, len(row.Steps), header.AutoCompaction)
}

func TestTheSpawnToolIsGivenTheSessionStoreBeforeTheTurnStarts(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	opts := armOpts(t)
	opts.dir, opts.task = dir, "hand the work to a child"
	built, _, err := buildRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	store := session.NewStore(filepath.Join(dir, ".tofu", "sessions"))
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)},
		}},
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-2", Name: "write", Arguments: json.RawMessage(`{"path":"note.txt","content":"a note"}`)},
		}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child wrote it"},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child reported"},
	}}

	config, _ := runConfig(opts, built, runtime{model: model, spend: turn.SpendSubscription, sessions: store})
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	if len(row.ChildIDs) != 1 {
		t.Fatalf("the parent names %v, want the one child", row.ChildIDs)
	}
	childID := row.ChildIDs[0]
	header, err := store.Header(childID)
	if err != nil {
		t.Fatalf("the child left no record of its own: %v", err)
	}
	if header.Parent != row.ID {
		t.Fatalf("the child record names parent %q, want %q", header.Parent, row.ID)
	}
	events, err := store.Body(childID)
	if err != nil {
		t.Fatalf("reading the child body: %v", err)
	}
	steps := 0
	for _, event := range events {
		if event.Kind == session.EventStep {
			steps++
		}
	}
	if steps != 2 {
		t.Fatalf("the child record carries %d steps, want the write and the answer once each", steps)
	}
	t.Logf("child %s recorded %d steps and %d events under a store the spawn tool had before the turn began", childID, steps, len(events))
}

func TestWithNoSearchKeyStoredTheTurnIsGivenFetchAndNoWebSearch(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	opts := armOpts(t)
	t.Chdir(root)
	names := toolNames(t, opts)
	if !slices.Contains(names, "fetch") {
		t.Errorf("a turn is given %v, and fetch is not among them", names)
	}
	if slices.Contains(names, "web_search") {
		t.Errorf("web_search is offered with no provider key stored: %v", names)
	}
	t.Logf("the tools a turn is given: %v", names)
}

func TestAProjectCarryingNoWebLibraryStillBuildsItsTools(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	built, _, err := buildRunTools(dir, toolSetFull)
	if err != nil {
		t.Fatalf("a project carrying no web library could not build its tools: %v", err)
	}
	for _, tool := range built {
		if tool.Name() == "fetch" || tool.Name() == "web_search" {
			t.Errorf("a project with no web library was given %s anyway", tool.Name())
		}
	}
}

type watchingModel struct {
	inner turn.Model
	store *session.Store
	child string
	found []bool
}

func (m *watchingModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	_, err := m.store.Header(m.child)
	m.found = append(m.found, err == nil)
	return m.inner.Ask(ctx, request)
}

func TestAChildLeavesItsRecordWhileTheParentsTurnIsStillRunning(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	opts := armOpts(t)
	opts.dir, opts.task, opts.turnID = dir, "hand the work to a child", "turn-parent"
	built, _, err := buildRunTools(dir, opts.toolSet)
	if err != nil {
		t.Fatalf("buildRunTools: %v", err)
	}
	store := session.NewStore(filepath.Join(dir, ".tofu", "sessions"))
	model := &watchingModel{store: store, child: "turn-parent-c1", inner: &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "spawn", Arguments: json.RawMessage(`{"task":"write note.txt","owns":["note.txt"]}`)},
		}},
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-2", Name: "write", Arguments: json.RawMessage(`{"path":"note.txt","content":"a note"}`)},
		}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child wrote it"},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the child reported"},
	}}}

	config, _ := runConfig(opts, built, runtime{model: model, spend: turn.SpendSubscription, sessions: store})
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	if len(model.found) != 4 {
		t.Fatalf("the turn asked %d times, want the parent twice and the child twice", len(model.found))
	}
	if model.found[0] {
		t.Errorf("the child's record was on disk before the parent spawned it")
	}
	if !model.found[1] || !model.found[2] {
		t.Errorf("the child asked twice and its record was on disk %v, want it there for both", model.found)
	}
	if len(row.ChildIDs) != 1 || row.ChildIDs[0] != model.child {
		t.Fatalf("the parent names %v, want the child %q", row.ChildIDs, model.child)
	}
	t.Logf("the child's record was on disk at asks %v, and the parent only returned after that", model.found)
}
