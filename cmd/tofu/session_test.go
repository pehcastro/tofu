package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/interface/cli"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/turn"
)

func recorded(t *testing.T, store *session.Store, id, task string, at time.Time, messages ...llm.Message) {
	t.Helper()
	events := []session.Event{{Kind: session.EventStep, Body: json.RawMessage(`{"index":1}`)}}
	for _, message := range messages {
		row := turn.MessageRow{Role: message.Role.String(), Content: message.Content, ToolCallID: message.ToolCallID}
		for _, call := range message.ToolCalls {
			row.ToolCalls = append(row.ToolCalls, turn.MessageToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
		}
		body, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, session.Event{Kind: session.EventMessage, Body: body})
	}
	header := session.Header{ID: id, At: at, Task: task, Root: id, Wire: wireSubscription, Model: "stub-model", Outcome: "stopped"}
	if err := store.Write(header, events); err != nil {
		t.Fatal(err)
	}
}

func sessionProject(t *testing.T) *session.Store {
	t.Helper()
	scratchProject(t)
	store, err := session.Open()
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func envelopeData(t *testing.T, printed string, into any) {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(printed), &envelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(envelope.Data, into); err != nil {
		t.Fatal(err)
	}
}

func sessionRun(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(""), &out, &errOut)
	t.Logf("tofu %s exited %d\n%s%s", strings.Join(args, " "), code, out.String(), errOut.String())
	return out.String(), errOut.String(), code
}

func TestSessionListPrintsTheRecordedSessionsNewestFirst(t *testing.T) {
	store := sessionProject(t)
	now := time.Now()
	recorded(t, store, "turn-older", "read the older task", now.Add(-3*time.Hour))
	recorded(t, store, "turn-newer", "read the newer task", now.Add(-10*time.Minute))
	if err := store.SetHead("turn-newer"); err != nil {
		t.Fatal(err)
	}

	out, _, code := sessionRun(t, "session", "list")
	if code != exitOK {
		t.Fatalf("tofu session list exited %d", code)
	}
	newer, older := strings.Index(out, "newer"), strings.Index(out, "older")
	if newer < 0 || older < 0 {
		t.Fatalf("the list names newer at %d and older at %d, want both", newer, older)
	}
	if newer > older {
		t.Errorf("the list prints older before newer, and the newest comes first")
	}
	for _, want := range []string{"2 sessions", "10m ago", "3h ago", "1 step", "stopped", "read the newer task"} {
		if !strings.Contains(out, want) {
			t.Errorf("the list does not say %q", want)
		}
	}
}

func TestASessionTheStoreCannotParseIsCountedAndNamed(t *testing.T) {
	store := sessionProject(t)
	recorded(t, store, "turn-good", "the readable one", time.Now())
	broken := store.Dir("turn-broken")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "header.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, code := sessionRun(t, "session", "list")
	if code != exitOK {
		t.Fatalf("tofu session list exited %d with one unreadable session, and one bad row is not a failed list", code)
	}
	if !strings.Contains(out, "unreadable") || !strings.Contains(out, "✗ 1") {
		t.Errorf("the list does not count the session it stepped over")
	}
	if !strings.Contains(out, "turn-broken") || !strings.Contains(out, "does not parse") {
		t.Errorf("the list does not name the unreadable session or say why")
	}
	if !strings.Contains(out, "the readable one") {
		t.Errorf("one unreadable session hid the readable ones")
	}

	asJSON, _, _ := sessionRun(t, "session", "list", jsonFlag)
	var report sessionListReport
	envelopeData(t, asJSON, &report)
	if len(report.Skipped) != 1 || report.Skipped[0].Session != "turn-broken" || report.Skipped[0].Reason == "" {
		t.Errorf("--json carries %d skipped sessions, want the one with its reason", len(report.Skipped))
	}
}

func TestSessionInfoPrintsTheHeaderAndTheCounts(t *testing.T) {
	store := sessionProject(t)
	recorded(t, store, "turn-one", "explain the repo", time.Now().Add(-time.Hour),
		llm.Message{Role: llm.RoleUser, Content: "explain the repo"},
		llm.Message{Role: llm.RoleAssistant, Content: "it is a harness"})

	out, _, code := sessionRun(t, "session", "info", "turn-one")
	if code != exitOK {
		t.Fatalf("tofu session info exited %d", code)
	}
	for _, want := range []string{"turn-one", "stopped", "explain the repo", "1 step", "2 messages", wireSubscription, "stub-model"} {
		if !strings.Contains(out, want) {
			t.Errorf("tofu session info does not say %q", want)
		}
	}

	asJSON, _, _ := sessionRun(t, "session", "info", "one", jsonFlag)
	var row sessionRow
	envelopeData(t, asJSON, &row)
	if row.ID != "turn-one" || row.Steps != 1 || row.Carried != 2 || row.Root != "turn-one" {
		t.Errorf("--json reads %+v, want the whole header and the counts", row)
	}
}

func TestSessionResumeSendsTheMessagesTheRecordHolds(t *testing.T) {
	dir := scratchProject(t)
	first := &sendModel{queued: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("call-1")}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the note is written"},
	}}
	var events eventLog
	stubbedTurn(dir, first)(t.Context(), onTheSubscription, "write a note", events.add)

	store, err := session.Open()
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.Head()
	if err != nil {
		t.Fatalf("the head after one turn: %v", err)
	}
	carry, err := resumeOf(store, head.ID)
	if err != nil {
		t.Fatalf("resuming %s: %v", head.ID, err)
	}
	if carry.Carried == 0 {
		t.Fatalf("the record of %s carries no message, so a resume would start over", head.ID)
	}

	second := &sendModel{queued: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "still here"}}}
	var resumedEvents eventLog
	resumedTurn(dir, second, nil, carry)(t.Context(), onTheSubscription, "what did you write", resumedEvents.add)

	if len(second.requests) != 1 {
		t.Fatalf("the resumed send asked the model %d times, want once", len(second.requests))
	}
	sent := second.requests[0].Messages
	var task, call, result bool
	for _, message := range sent {
		t.Logf("the resumed send carries role=%s tool_call_id=%s tool_calls=%d content=%.40q",
			message.Role, message.ToolCallID, len(message.ToolCalls), message.Content)
		switch {
		case message.Role == llm.RoleUser && strings.Contains(message.Content, "write a note"):
			task = true
		case len(message.ToolCalls) == 1 && message.ToolCalls[0].ID == "call-1":
			call = true
		case message.Role == llm.RoleTool && message.ToolCallID == "call-1":
			result = true
		}
	}
	if !task || !call || !result {
		t.Errorf("the resumed send carries the recorded task %v, its tool call %v and the result %v, want all three", task, call, result)
	}
	if carry.Session != head.ID {
		t.Errorf("the resume names %s, want the head %s", carry.Session, head.ID)
	}
}

func liveAppSession(dir string, model turn.Model) *appSession {
	return newAppSession(dir, func(runOpts) (appWire, error) {
		return wireOn(model), nil
	}, nil, time.Now, sessionResume{})
}

func TestSlashNewDropsWhatIsCarriedAndSlashResumeTakesTheSessionBack(t *testing.T) {
	dir := scratchProject(t)
	first := &sendModel{queued: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "wrote it"}}}
	live := liveAppSession(dir, first)
	var events eventLog
	live.run(t.Context(), onTheSubscription, "write a note", events.add)
	if live.ID() == "" || len(live.Carried()) == 0 {
		t.Fatalf("one turn left session %q carrying %d messages", live.ID(), len(live.Carried()))
	}

	ran := live.ID()
	if note := live.startFresh(); live.ID() != "" || len(live.Carried()) != 0 {
		t.Fatalf("/new said %q and left session %q carrying %d messages", note, live.ID(), len(live.Carried()))
	}

	note, chat := live.resume(ran)
	if live.ID() != ran || len(live.Carried()) == 0 || len(chat) == 0 {
		t.Fatalf("/resume %s said %q, took %q and carried %d messages and %d chat events back", ran, note, live.ID(), len(live.Carried()), len(chat))
	}
	if !strings.Contains(note, live.ID()) {
		t.Errorf("/resume said %q and does not name the session it took", note)
	}

	second := &sendModel{queued: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "still here"}}}
	live.engine.open = func(runOpts) (appWire, error) {
		return wireOn(second), nil
	}
	live.run(t.Context(), onTheSubscription, "what did you write", events.add)
	if len(second.requests) != 1 {
		t.Fatalf("the resumed turn asked the model %d times, want once", len(second.requests))
	}
	carried := false
	for _, message := range second.requests[0].Messages {
		if message.Role == llm.RoleUser && strings.Contains(message.Content, "write a note") {
			carried = true
		}
	}
	if !carried {
		t.Error("the turn after /resume does not carry the first task")
	}
}

func TestResumingATurnInterruptedMidToolCallSendsAListBothWiresAccept(t *testing.T) {
	store := sessionProject(t)
	recorded(t, store, "turn-cut", "read three files", time.Now(),
		llm.Message{Role: llm.RoleUser, Content: "read three files"},
		llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
			{ID: "call-2", Name: "read", Arguments: json.RawMessage(`{"path":"b.txt"}`)},
		}},
		llm.Message{Role: llm.RoleTool, ToolCallID: "call-1", Content: "a"})

	carry, err := resumeOf(store, "turn-cut")
	if err != nil {
		t.Fatal(err)
	}
	answered := map[string]bool{}
	for _, message := range carry.messages {
		if message.ToolCallID != "" {
			answered[message.ToolCallID] = true
		}
	}
	for _, message := range carry.messages {
		t.Logf("resumed role=%s tool_call_id=%s tool_calls=%d", message.Role, message.ToolCallID, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			if !answered[call.ID] {
				t.Errorf("the resumed list carries the call %s with no result, which both wires refuse", call.ID)
			}
		}
	}

	if _, err := (anthropic.Request{Model: "m", System: []string{"s"}, Messages: carry.messages}).Encode(true); err != nil {
		t.Errorf("the anthropic wire refuses the resumed list: %v", err)
	}
	if _, err := (codex.Request{Model: "m", Instructions: "s", Messages: carry.messages}).Encode(nil); err != nil {
		t.Errorf("the codex wire refuses the resumed list: %v", err)
	}
}

func TestContinueWithNoSessionRecordedPrintsNothingBeforeTheApp(t *testing.T) {
	scratchProject(t)
	out, errOut, _ := sessionRun(t, "--continue")
	if out != "" {
		t.Errorf("tofu --continue with nothing recorded printed before the app, and the app says it starts fresh:\n%s", out)
	}
	if !strings.Contains(errOut, noTerminal) {
		t.Errorf("tofu --continue stopped for a reason other than the missing terminal:\n%s", errOut)
	}
}

func TestContinueTakesTheHeadWithNoListAndNoQuestion(t *testing.T) {
	store := sessionProject(t)
	recorded(t, store, "turn-old", "the older task", time.Now().Add(-time.Hour))
	recorded(t, store, "turn-head", "the head task", time.Now(),
		llm.Message{Role: llm.RoleUser, Content: "the head task"})
	if err := store.SetHead("turn-head"); err != nil {
		t.Fatal(err)
	}

	if out, _, _ := sessionRun(t, "--continue"); out != "" {
		t.Errorf("tofu --continue printed before the app, and the app carries the resume note:\n%s", out)
	}
	asJSON, _, _ := sessionRun(t, "--continue", jsonFlag)
	var took sessionResume
	envelopeData(t, asJSON, &took)
	if took.Session != "turn-head" {
		t.Errorf("tofu --continue --json took %q, want the head turn-head:\n%s", took.Session, asJSON)
	}
}

func TestAWordThatIsNoSubcommandIsReadAsOneSessionHandle(t *testing.T) {
	sessionProject(t)
	out, errOut, code := sessionRun(t, "session", "tree", "turn-one")
	if code != exitUsage {
		t.Errorf("two operands with no subcommand exited %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut, "2 operands, want <name|id>") {
		t.Errorf("the refusal does not say it wanted one handle:\n%s", errOut)
	}
	if out != "" {
		t.Errorf("an unknown subcommand printed to stdout:\n%s", out)
	}
}

func TestSessionListJSONCarriesWhatTheLineCollapsed(t *testing.T) {
	store := sessionProject(t)
	long := "a task whose first line is far longer than the column the readable list keeps for it"
	recorded(t, store, "turn-one", long, time.Now())
	if err := store.SetHead("turn-one"); err != nil {
		t.Fatal(err)
	}

	out, _, _ := sessionRun(t, "session", "list")
	if strings.Contains(out, long) {
		t.Errorf("the readable line was not expected to hold the whole task:\n%s", out)
	}
	asJSON, _, code := sessionRun(t, "session", "list", jsonFlag)
	if code != exitOK {
		t.Fatalf("tofu session list --json exited %d", code)
	}
	var report sessionListReport
	envelopeData(t, asJSON, &report)
	if len(report.Sessions) != 1 || report.Sessions[0].Task != long {
		t.Fatalf("--json reads %d sessions, want the one with its whole task", len(report.Sessions))
	}
	if report.Head != "turn-one" || !report.Sessions[0].Head || report.Sessions[0].Model != "stub-model" {
		t.Errorf("--json reads head %q and row %+v, want the head and the fields the line drops", report.Head, report.Sessions[0])
	}
}

func nameOf(t *testing.T, store *session.Store, id string) string {
	t.Helper()
	header, err := store.Header(id)
	if err != nil {
		t.Fatalf("header of %s: %v", id, err)
	}
	if header.Name == nil {
		t.Fatalf("%s was written with no name, and the id is all a person could type", id)
	}
	return *header.Name
}

func TestASessionListedNowCarriesAGeneratedNameAndTheIDIsStillInJSON(t *testing.T) {
	store := sessionProject(t)
	recorded(t, store, "turn-one", "explain the repo", time.Now())
	name := nameOf(t, store, "turn-one")

	out, _, code := sessionRun(t, "session", "list")
	if code != exitOK {
		t.Fatalf("tofu session list exited %d", code)
	}
	if !strings.Contains(out, name) {
		t.Errorf("the list does not print the name %q a person would type:\n%s", name, out)
	}

	asJSON, _, _ := sessionRun(t, "session", "list", jsonFlag)
	var report sessionListReport
	envelopeData(t, asJSON, &report)
	if len(report.Sessions) != 1 || report.Sessions[0].ID != "turn-one" || report.Sessions[0].Name != name {
		t.Errorf("--json reads %+v, want the id and the name together", report.Sessions)
	}
}

func TestRenameThenInfoAndResumeFindTheSessionByTheNewName(t *testing.T) {
	store := sessionProject(t)
	recorded(t, store, "turn-one", "explain the repo", time.Now(),
		llm.Message{Role: llm.RoleUser, Content: "explain the repo"})

	out, errOut, code := sessionRun(t, "session", "rename", "turn-one", "The Gate Work")
	if code != exitOK {
		t.Fatalf("tofu session rename exited %d: %s", code, errOut)
	}
	if !strings.Contains(out, "the-gate-work") || !strings.Contains(out, "turn-one") {
		t.Errorf("the rename does not print the new name beside the id:\n%s", out)
	}

	found, _, code := sessionRun(t, "session", "info", "the-gate-work")
	if code != exitOK {
		t.Fatalf("tofu session info the-gate-work exited %d", code)
	}
	if !strings.Contains(found, "turn-one") {
		t.Errorf("tofu session info by name does not reach the session:\n%s", found)
	}

	byName, err := resumeOf(store, "the-gate-work")
	if err != nil {
		t.Fatalf("resume by name: %v", err)
	}
	byID, err := resumeOf(store, "turn-one")
	if err != nil {
		t.Fatalf("resume by id: %v", err)
	}
	if byName.Session != byID.Session || byName.Carried != byID.Carried {
		t.Errorf("resuming by name took %+v and by id took %+v, want the same session", byName, byID)
	}

	asJSON, _, _ := sessionRun(t, "session", "resume", "the-gate-work", jsonFlag)
	var carry sessionResume
	envelopeData(t, asJSON, &carry)
	if carry.Session != "turn-one" || carry.Name != "the-gate-work" {
		t.Errorf("--json reads %+v, want the id and the name together", carry)
	}
}

func TestTwoSessionsSharingANameAreBothListedWithTheirDatesRatherThanOneBeingPicked(t *testing.T) {
	store := sessionProject(t)
	now := time.Now()
	recorded(t, store, "turn-older", "the older gate work", now.Add(-3*time.Hour))
	recorded(t, store, "turn-newer", "the newer gate work", now.Add(-time.Minute))
	for _, id := range []string{"turn-older", "turn-newer"} {
		if _, _, code := sessionRun(t, "session", "rename", id, "gate"); code != exitOK {
			t.Fatalf("renaming %s exited %d", id, code)
		}
	}

	_, errOut, code := sessionRun(t, "session", "info", "gate")
	if code == exitOK {
		t.Fatal("an ambiguous name was answered with one session, and it names two")
	}
	for _, want := range []string{"2 sessions are called gate", "turn-older (3h ago)", "turn-newer (1m ago)", "name one by id"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, errOut)
		}
	}
}

func TestTheSessionsAlreadyOnDiskListWithNoNameRatherThanFailing(t *testing.T) {
	store := sessionProject(t)
	recorded(t, store, "turn-named", "written with a name", time.Now())
	for name, body := range map[string]string{
		"header.json": `{"id":"turn-old","at":"2026-09-01T10:00:00Z","task":"written before the name","root":"turn-old"}`,
		"body.jsonl":  `{"at":"2026-09-01T10:00:01Z","kind":"step","body":{"index":1}}` + "\n",
	} {
		if err := sys.WriteFile(filepath.Join(store.Dir("turn-old"), name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	report, err := sessionListing(store, session.DefaultSettings().Lifetime, time.Now())
	if err != nil {
		t.Fatalf("rendering the recorded sessions: %v", err)
	}
	if report.Expired != 0 {
		t.Fatalf("%d recorded sessions are expired under the default lifetime", report.Expired)
	}
	unnamed := 0
	for _, row := range report.Sessions {
		if row.Name == "" {
			unnamed++
		}
	}
	text := strings.Join(sessionListLines(cli.Page{Width: konst.ProseWidthChars}, report, time.Now()), "\n")
	t.Logf("%d sessions, %d of them written before the name, %d skipped\n%s",
		len(report.Sessions), unnamed, len(report.Skipped), text)
	if unnamed == 0 {
		t.Fatal("no session on disk reads as written before the name, so this proves nothing about the old records")
	}
	for _, row := range report.Sessions {
		if row.Name == "" && !strings.Contains(text, sessionShortID(row.ID)) {
			t.Fatalf("the session %s has no name and no id on its line", row.ID)
		}
	}
}

func readingSession(t *testing.T, id string, paths ...string) {
	t.Helper()
	store := sessionProject(t)
	step := turn.StepRow{Index: 1, AssistantText: "looking at the store"}
	for i, path := range paths {
		step.ToolCalls = append(step.ToolCalls, turn.ToolCallRow{
			Tool:         "read",
			Args:         json.RawMessage(`{"path":"` + path + `"}`),
			ResultBytes:  1024 * (i + 1),
			ResultHandle: "artifact-" + path,
		})
	}
	body, err := json.Marshal(step)
	if err != nil {
		t.Fatal(err)
	}
	header := session.Header{ID: id, At: time.Now(), Task: "read three files", Root: id, Outcome: "stopped"}
	if err := store.Write(header, []session.Event{{Kind: session.EventStep, Body: body}}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionReadsNamesEveryFileTheTurnReadWithItsSizeAndWhatWasSaid(t *testing.T) {
	readingSession(t, "turn-reader", "internal/session/store.go", "internal/turn/loop.go", "cmd/tofu/session.go")

	out, _, code := sessionRun(t, "session", "reads", "turn-reader")
	if code != exitOK {
		t.Fatalf("tofu session reads exited %d", code)
	}
	for _, want := range []string{"3 reads", "internal/session/store.go", "internal/turn/loop.go", "cmd/tofu/session.go", "1.0 KB", "2.0 KB", "3.0 KB", "looking at the store"} {
		if !strings.Contains(out, want) {
			t.Errorf("tofu session reads does not say %q", want)
		}
	}
}

func TestSessionReadsJSONCarriesThePathTheSizeAndWhereTheReasoningCameFrom(t *testing.T) {
	readingSession(t, "turn-reader", "internal/session/store.go")

	out, _, code := sessionRun(t, "session", "reads", "turn-reader", jsonFlag)
	if code != exitOK {
		t.Fatalf("tofu session reads --json exited %d", code)
	}
	var report sessionReadsReport
	envelopeData(t, out, &report)
	if len(report.Reads) != 1 {
		t.Fatalf("--json carries %d reads, want 1", len(report.Reads))
	}
	read := report.Reads[0]
	if read.Source != "internal/session/store.go" || read.Bytes != 1024 || read.Artifact != "artifact-internal/session/store.go" {
		t.Errorf("--json reads %+v, want the path, the size and the artifact holding it", read)
	}
	if read.ReasoningSource != session.ReasoningFromAssistantText || read.Reasoning != "looking at the store" {
		t.Errorf("--json says the reasoning is %q from %q", read.Reasoning, read.ReasoningSource)
	}
}

func TestASessionEndedIsMarkedEndedAnOpenOneIsNotAndTheEndedOneStillResumes(t *testing.T) {
	store := sessionProject(t)
	at := time.Now()
	recorded(t, store, "turn-ended", "the ended one", at, llm.Message{Role: llm.RoleUser, Content: "the ended one"})
	recorded(t, store, "turn-open", "the open one", at, llm.Message{Role: llm.RoleUser, Content: "the open one"})
	if _, err := store.End("turn-ended", session.EndedByNew, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	ended, _, code := sessionRun(t, "session", "info", "turn-ended", jsonFlag)
	if code != exitOK {
		t.Fatalf("tofu session info exited %d", code)
	}
	var endedRow sessionRow
	envelopeData(t, ended, &endedRow)
	if endedRow.EndedAt == nil || endedRow.EndReason != session.EndedByNew {
		t.Errorf("the ended session reads %+v, want an end time and the reason new", endedRow)
	}
	open, _, _ := sessionRun(t, "session", "info", "turn-open", jsonFlag)
	var openRow sessionRow
	envelopeData(t, open, &openRow)
	if openRow.EndedAt != nil {
		t.Errorf("a session nobody ended reads %+v", openRow)
	}

	carry, err := resumeOf(store, "turn-ended")
	if err != nil {
		t.Fatalf("an ended session does not resume: %v", err)
	}
	if carry.Session != "turn-ended" || len(carry.messages) != 1 {
		t.Errorf("the resume of an ended session carries %+v", carry)
	}
}

func TestASessionPastTheLifetimeIsListedAsExpiredAndIsStillOnDisk(t *testing.T) {
	store := sessionProject(t)
	now := time.Now()
	recorded(t, store, "turn-old", "the old one", now.Add(-40*sessionDay))
	recorded(t, store, "turn-young", "the young one", now.Add(-2*sessionDay))

	report, err := sessionListing(store, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	expired := map[string]bool{}
	for _, row := range report.Sessions {
		expired[row.ID] = row.Expired
	}
	if !expired["turn-old"] || expired["turn-young"] {
		t.Errorf("expired reads %v, want only turn-old", expired)
	}
	if report.Expired != 1 {
		t.Errorf("the list counts %d expired, want 1", report.Expired)
	}
	text := strings.Join(sessionListLines(cli.Page{Width: konst.ProseWidthChars}, report, now), "\n")
	if !strings.Contains(text, "1 past 30d, kept") {
		t.Errorf("the list says nothing about the expired session:\n%s", text)
	}
	for _, id := range []string{"turn-old", "turn-young"} {
		if _, err := os.Stat(filepath.Join(store.Dir(id), "session.json")); err != nil {
			t.Fatalf("%s was deleted: %v", id, err)
		}
	}
	t.Logf("%s", text)
}

func TestTheDefaultLifetimeExpiresNothingOnTheList(t *testing.T) {
	store := sessionProject(t)
	now := time.Now()
	recorded(t, store, "turn-ancient", "written years ago", now.Add(-4000*sessionDay))

	report, err := sessionListing(store, session.DefaultSettings().Lifetime, now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Expired != 0 || report.Lifetime != session.LifetimeNever {
		t.Fatalf("the default lifetime is %s and expired %d sessions", report.Lifetime, report.Expired)
	}
}

func TestSessionResumeJSONNamesWhatItCarries(t *testing.T) {
	store := sessionProject(t)
	recorded(t, store, "turn-one", "explain the repo", time.Now(),
		llm.Message{Role: llm.RoleUser, Content: "explain the repo"})

	out, _, _ := sessionRun(t, "session", "resume", "turn-one", jsonFlag)
	var carry sessionResume
	envelopeData(t, out, &carry)
	if carry.Session != "turn-one" || carry.Carried != 1 || carry.Steps != 1 {
		t.Errorf("--json reads %+v, want the session it resumes and what it carries", carry)
	}
}

func TestTheForkSentenceReadsTheSameInSessionInfoAndInContext(t *testing.T) {
	store := sessionProject(t)
	const id, into = "turn-forker", "turn-forker-f2"
	row := turn.Row{
		ID: id, Schema: turn.SchemaVersion, At: time.Now(), Task: "read the repo",
		Root: id, Outcome: turn.OutcomeForked, ForkedInto: into,
		Steps: []turn.StepRow{{Index: 1, Fork: &turn.Fork{
			Kind: turn.ForkContinuation, Into: into, TokensBefore: 1326, TokensAfter: 1475,
		}}},
	}
	header, events, err := row.Record()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(header, events); err != nil {
		t.Fatal(err)
	}

	sentence := forkLineOf(t, contextRun(t, id))
	info, _, code := sessionRun(t, "session", "info", id)
	if code != exitOK {
		t.Fatalf("tofu session info exited %d", code)
	}
	if !strings.Contains(strings.Join(strings.Fields(info), " "), sentence) {
		t.Fatalf("tofu context says\n%s\nand tofu session info says\n%s", sentence, info)
	}
}

func TestAFailedCallAndAFailedTurnKeepTheirReasonInTraceAndInfo(t *testing.T) {
	dir := scratchProject(t)
	model := &sendModel{queued: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
		{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"echo first; echo the disk is gone >&2; exit 3"}`)},
	}}}}
	var events eventLog
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "fail twice", events.add)
	store, err := session.Open()
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.Head()
	if err != nil {
		t.Fatal(err)
	}

	traced, _, _ := sessionRun(t, "session", "trace", head.ID, jsonFlag)
	var trace sessionTraceReport
	envelopeData(t, traced, &trace)
	if len(trace.Calls) != 1 || trace.Calls[0].Outcome != session.ToolOutcomeFailed {
		t.Fatalf("the trace carries %+v, want the one failed bash call", trace.Calls)
	}
	if reason := trace.Calls[0].Reason; !strings.Contains(reason, "exit 3") || !strings.Contains(reason, "the disk is gone") {
		t.Errorf("the failed call carries the reason %q, want the exit code and the last line it printed", reason)
	}
	if !strings.Contains(trace.Error, "no more decisions queued") {
		t.Errorf("the trace carries the session error %q, want the model's error", trace.Error)
	}
	if len(trace.Failures) != 1 || !strings.Contains(trace.Failures[0].Error, "no more decisions queued") || trace.Failures[0].Turn == "" {
		t.Errorf("the trace lists the failures %+v, want the one turn that ended in error", trace.Failures)
	}

	info, _, _ := sessionRun(t, "session", "info", head.ID, jsonFlag)
	var row sessionRow
	envelopeData(t, info, &row)
	if row.Outcome != "error" || !strings.Contains(row.Error, "no more decisions queued") {
		t.Errorf("info carries outcome %q and error %q, want the error and its reason", row.Outcome, row.Error)
	}
}

type thenRefused struct {
	sendModel
	refused error
}

func (m *thenRefused) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	if len(m.queued) == 0 {
		m.requests = append(m.requests, request)
		return llm.Decision{}, m.refused
	}
	return m.sendModel.Ask(ctx, request)
}

func TestSessionRequestPrintsTheFailingRequestAsSentAndTheWholeErrorBody(t *testing.T) {
	dir := scratchProject(t)
	detail := `{"type":"error","error":{"type":"invalid_request_error","message":"messages.3: tool_use ids were found without tool_result blocks immediately after: call-a"}}`
	model := &thenRefused{sendModel: sendModel{queued: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
		{ID: "call-a", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
		{ID: "call-b", Name: "read", Arguments: json.RawMessage(`{"path":"b.txt"}`)},
	}}}}, refused: &transport.Error{Kind: transport.KindBadRequest, Op: "anthropic.Ask", Status: 400, Detail: detail}}
	var events eventLog
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "read both", events.add)
	store, err := session.Open()
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.Head()
	if err != nil {
		t.Fatal(err)
	}

	traced, _, _ := sessionRun(t, "session", "trace", head.ID, jsonFlag)
	var trace sessionTraceReport
	envelopeData(t, traced, &trace)
	failing := ""
	for _, request := range trace.Requests {
		if request.Error != "" {
			failing = request.Request
		}
	}
	if len(trace.Requests) != 2 || failing == "" {
		t.Fatalf("the trace lists the requests %+v, want the two sent and the second marked failed", trace.Requests)
	}

	printed, _, code := sessionRun(t, "session", "request", head.ID, failing[len(failing)-6:], jsonFlag)
	if code != exitOK {
		t.Fatalf("tofu session request exited %d", code)
	}
	var sent sessionRequestReport
	envelopeData(t, printed, &sent)
	var shape []string
	for _, message := range sent.Messages {
		shape = append(shape, strings.Join(strings.Fields(message.Role+" "+strings.Join(message.Calls, ",")+" "+message.Answers), " "))
	}
	if got := strings.Join(shape, " | "); !strings.HasSuffix(got, "user | assistant call-a,call-b | tool call-a | tool call-b") {
		t.Errorf("the request as sent reads %q, want the task, the two calls and both results in order", got)
	}
	if !strings.Contains(sent.Error, detail) {
		t.Errorf("the request carries the error %q, want the whole provider body", sent.Error)
	}
	if text, _, _ := sessionRun(t, "session", "request", head.ID, failing); !strings.Contains(text, "call-a") || !strings.Contains(text, "invalid_request_error") {
		t.Errorf("tofu session request does not print the calls and the error body:\n%s", text)
	}
}

func contextRun(t *testing.T, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := run(append([]string{"context"}, args...), strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("tofu context %v exited %d: %s", args, code, errOut.String())
	}
	return out.String()
}

func forkLineOf(t *testing.T, printed string) string {
	t.Helper()
	for _, line := range strings.Split(printed, "\n") {
		if label, fact, found := strings.Cut(strings.TrimSpace(line), " "); found && label == "fork" {
			return strings.TrimSpace(fact)
		}
	}
	t.Fatalf("nothing printed here is a fork line:\n%s", printed)
	return ""
}
