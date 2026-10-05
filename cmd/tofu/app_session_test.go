package main

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
	"testing"
	"time"

	"tofu/interface/tui"
	"tofu/interface/tui/paste"
	"tofu/interface/tui/session"
	"tofu/internal/llm"
	sessionstore "tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

type sendModel struct {
	queued   []llm.Decision
	requests []llm.Request
}

func (m *sendModel) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	m.requests = append(m.requests, request)
	if len(m.queued) == 0 {
		return llm.Decision{}, errors.New("sendModel: no more decisions queued")
	}
	next := m.queued[0]
	m.queued = m.queued[1:]
	return next, nil
}

type rememberingModel struct {
	fact  string
	reads int
}

func (m *rememberingModel) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	for _, message := range request.Messages {
		if message.Role != llm.RoleSystem && strings.Contains(message.Content, m.fact) {
			return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "from what I already read, " + m.fact}, nil
		}
	}
	m.reads++
	return llm.Decision{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
		{ID: "call-" + strconv.Itoa(m.reads), Name: "read", Arguments: json.RawMessage(`{"path":"notes.txt"}`)},
	}}, nil
}

type eventLog struct {
	mu     sync.Mutex
	events []tui.Event
}

func (l *eventLog) add(event tui.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *eventLog) all() []tui.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.events)
}

func TestASecondSendCarriesTheFirstExchangeAndItsToolCalls(t *testing.T) {
	dir := scratchProject(t)
	model := &sendModel{queued: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("call-1")}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "the repo is a go harness"},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "written down"},
	}}
	send := stubbedTurn(dir, model)
	var events eventLog
	send(t.Context(), onTheSubscription, "explain the repo", events.add)
	send(t.Context(), onTheSubscription, "write those findings down", events.add)

	if len(model.requests) != 3 {
		t.Fatalf("the model was asked %d times, want two for the first send and one for the second", len(model.requests))
	}
	second := model.requests[2].Messages
	var task, answer, call, result bool
	for _, message := range second {
		t.Logf("second send carries role=%s tool_call_id=%s tool_calls=%d content=%.40q",
			message.Role, message.ToolCallID, len(message.ToolCalls), message.Content)
		switch {
		case message.Role == llm.RoleUser && strings.HasSuffix(message.Content, "explain the repo"):
			task = true
		case message.Role == llm.RoleAssistant && message.Content == "the repo is a go harness":
			answer = true
		case message.Role == llm.RoleAssistant && len(message.ToolCalls) == 1 && message.ToolCalls[0].ID == "call-1":
			call = true
		case message.Role == llm.RoleTool && message.ToolCallID == "call-1":
			result = true
		}
	}
	if !task || !answer {
		t.Errorf("the second send carries the first task %v and the first answer %v, want both", task, answer)
	}
	if !call || !result {
		t.Errorf("the second send carries the first tool call %v and its result %v, want both", call, result)
	}
	last := second[len(second)-1]
	if last.Role != llm.RoleUser || !strings.HasSuffix(last.Content, "write those findings down") {
		t.Errorf("the last message is %s %q, want the second task", last.Role, last.Content)
	}
	if !strings.Contains(last.Content, "today's date: ") {
		t.Errorf("the second send was not told where it is: %q", last.Content)
	}
}

type stoppingModel struct {
	cancel   context.CancelFunc
	queued   []llm.Decision
	requests []llm.Request
}

func (m *stoppingModel) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	if err := ctx.Err(); err != nil {
		return llm.Decision{}, err
	}
	m.requests = append(m.requests, request)
	if len(m.queued) == 0 {
		m.cancel()
		return llm.Decision{}, context.Canceled
	}
	next := m.queued[0]
	m.queued = m.queued[1:]
	return next, nil
}

func TestASendStoppedPartWayCarriesItsWorkIntoThePleaseContinue(t *testing.T) {
	dir := scratchProject(t)
	model := &stoppingModel{queued: []llm.Decision{{
		Build:     "stub-model",
		Outcome:   llm.OutcomeToolCalls,
		ToolCalls: []llm.ToolCall{writeNote("call-1"), writeNote("call-2")},
	}}}
	stopped, cancel := context.WithCancel(context.Background())
	defer cancel()
	model.cancel = cancel
	send := stubbedTurn(dir, model)

	var interrupted eventLog
	send(stopped, onTheSubscription, "explain the repo", interrupted.add)
	for _, event := range interrupted.all() {
		if event.Kind == tui.EventFailure {
			t.Fatalf("stopping a send reported a failure: %s", event.Text)
		}
	}

	var resumed eventLog
	send(t.Context(), onTheSubscription, "please continue", resumed.add)
	if len(model.requests) != 3 {
		t.Fatalf("the model was asked %d times, want two for the stopped send and one for the continue", len(model.requests))
	}

	continued := model.requests[2].Messages
	var task, calls, results int
	for _, message := range continued {
		t.Logf("the continue carries role=%s tool_call_id=%s tool_calls=%d content=%.40q",
			message.Role, message.ToolCallID, len(message.ToolCalls), message.Content)
		if message.Role == llm.RoleUser && strings.Contains(message.Content, "explain the repo") {
			task++
		}
		calls += len(message.ToolCalls)
		if message.Role == llm.RoleTool {
			results++
		}
	}
	if task != 1 {
		t.Errorf("the continue names the stopped task %d times, want once: a model that never saw it starts over", task)
	}
	if calls != 2 || results != 2 {
		t.Errorf("the continue carries %d tool calls and %d results, want the two the stopped send made", calls, results)
	}
	last := continued[len(continued)-1]
	if last.Role != llm.RoleUser || !strings.HasSuffix(last.Content, "please continue") {
		t.Errorf("the continue ends with %s %q, want the new task", last.Role, last.Content)
	}
}

func TestASecondSendAnswersFromWhatTheFirstLearnedWithoutATool(t *testing.T) {
	dir := scratchProject(t)
	fact := "tofu is a go harness whose code owns the loop"
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(fact+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	send := stubbedTurn(dir, &rememberingModel{fact: fact})

	var first eventLog
	send(t.Context(), onTheSubscription, "explain the repo", first.add)
	var second eventLog
	send(t.Context(), onTheSubscription, "write those findings down", second.add)

	firstCalls, secondCalls := 0, 0
	for _, event := range first.all() {
		if event.Kind == tui.EventToolCall {
			firstCalls++
		}
	}
	answered := ""
	for _, event := range second.all() {
		switch event.Kind {
		case tui.EventToolCall:
			secondCalls++
			t.Errorf("the second send ran %s %q, and it already knew the answer", event.Tool, event.Text)
		case tui.EventText:
			answered = event.Text
		}
	}
	t.Logf("tool calls: first send %d, second send %d", firstCalls, secondCalls)
	if firstCalls != 1 {
		t.Fatalf("the first send made %d tool calls, want the one that learned the fact", firstCalls)
	}
	if !strings.Contains(answered, fact) {
		t.Fatalf("the second send answered %q, want it to repeat what it read", answered)
	}
}

func TestTwoSendsAppendToOneSessionBody(t *testing.T) {
	dir := scratchProject(t)
	model := &sendModel{queued: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "first answer"},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "second answer"},
	}}
	send := stubbedTurn(dir, model)
	var events eventLog
	send(t.Context(), onTheSubscription, "first task", events.add)
	send(t.Context(), onTheSubscription, "second task", events.add)

	store, err := sessionstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	listing, err := store.Listing()
	if err != nil {
		t.Fatal(err)
	}
	headers := listing.Sessions
	if len(headers) != 1 {
		for _, header := range headers {
			t.Logf("session %s task %q", header.ID, header.Task)
		}
		t.Fatalf("%d sessions on disk, %d unreadable, want the two sends in one", len(headers), len(listing.Skipped))
	}
	body, err := store.Body(headers[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	steps, outcomes, written := 0, 0, ""
	for _, event := range body {
		written += string(event.Body)
		switch event.Kind {
		case sessionstore.EventStep:
			steps++
		case sessionstore.EventOutcome:
			outcomes++
		}
	}
	t.Logf("session %s holds %d step events and %d outcome events", headers[0].ID, steps, outcomes)
	if steps != 2 || outcomes != 2 {
		t.Fatalf("the body holds %d steps and %d outcomes, want one of each per send", steps, outcomes)
	}
	if !strings.Contains(written, "first task") || !strings.Contains(written, "second task") {
		t.Fatalf("the body does not hold both tasks:\n%s", written)
	}
}

func screenshotBoard(t *testing.T, live *appSession) paste.Board {
	t.Helper()
	return paste.Default(paste.Board{
		Read: func() (sys.Clipboard, error) {
			return sys.Clipboard{Kind: sys.ClipboardImage, PNG: []byte("pretend this is a screenshot")}, nil
		},
		Dir:      live.pendingSessionDir,
		Recorded: live.recordAttachment,
	})
}

func TestAnImagePastedBeforeTheFirstSendLandsInTheSessionThatSendCreates(t *testing.T) {
	dir := scratchProject(t)
	model := &sendModel{queued: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "seen"}}}
	live := newAppSession(dir, func(runOpts) (appWire, error) {
		return wireOn(model), nil
	}, nil, time.Now, sessionResume{})

	board := screenshotBoard(t, live)
	msg := board.Attach(1)()
	outcome, ok := msg.(paste.Outcome)
	if !ok || outcome.State != paste.Ready {
		t.Fatalf("the paste returned %#v before any send, want a ready image", msg)
	}
	pending := live.pendingID()

	var events eventLog
	live.run(t.Context(), onTheSubscription, "look at what I pasted", events.add)
	if live.id != pending {
		t.Fatalf("send minted %s, want the id the paste already used: %s", live.id, pending)
	}

	store, err := sessionstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.AttachmentDir(live.id), outcome.Name)); err != nil {
		t.Fatalf("%s is not kept for the session send created: %v", outcome.Name, err)
	}
	held, err := os.ReadDir(store.Dir(live.id))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range held {
		names = append(names, entry.Name())
	}
	if want := []string{"events.jsonl", "session.json"}; !slices.Equal(names, want) {
		t.Fatalf("the session folder holds %v, want %v alone", names, want)
	}
}

func TestACronFileThatDidNotLoadIsLeftAsItIsBySends(t *testing.T) {
	dir := scratchProject(t)
	model := &sendModel{queued: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "first"},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "second"},
	}}
	live := newAppSession(dir, func(runOpts) (appWire, error) {
		return wireOn(model), nil
	}, nil, time.Now, sessionResume{})
	var first eventLog
	live.run(t.Context(), onTheSubscription, "first task", first.add)

	store, err := sessionstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	book := cronFile(store, live.id)
	corrupt := "{not json"
	if err := os.WriteFile(book, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := live.loadCron(live.id); err == nil {
		t.Fatal("a corrupt cron.json loaded")
	}

	var second eventLog
	live.run(t.Context(), onTheSubscription, "second task", second.add)
	body, err := os.ReadFile(book)
	t.Logf("cron.json after the send: %q", body)
	if err != nil || string(body) != corrupt {
		t.Fatalf("the send overwrote a cron.json that did not load: %q, %v", body, err)
	}
	if len(model.requests) != 2 {
		t.Fatalf("the model was asked %d times, want the second send to run", len(model.requests))
	}
	said := false
	for _, event := range second.all() {
		if event.Kind == tui.EventFailure {
			t.Fatalf("the refused write failed the turn: %s", event.Text)
		}
		said = said || event.Kind == tui.EventNote && strings.Contains(event.Text, book)
	}
	if !said {
		t.Fatalf("no note names %s as left as it is: %+v", book, second.all())
	}
}

func TestAPastedImageReachesTheRequestSentToTheModel(t *testing.T) {
	dir := scratchProject(t)
	model := &sendModel{queued: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "seen"}}}
	live := newAppSession(dir, func(runOpts) (appWire, error) {
		return wireOn(model), nil
	}, nil, time.Now, sessionResume{})

	board := screenshotBoard(t, live)
	msg := board.Attach(1)()
	outcome := msg.(paste.Outcome)

	var events eventLog
	live.run(t.Context(), onTheSubscription, "look at "+session.ImageToken(1), events.add)

	if len(model.requests) != 1 {
		t.Fatalf("the model was asked %d times, want one", len(model.requests))
	}
	last := model.requests[0].Messages[len(model.requests[0].Messages)-1]
	if len(last.Images) != 1 {
		t.Fatalf("the request carries %d images, want one", len(last.Images))
	}
	if last.Images[0].MediaType != "image/png" || string(last.Images[0].Data) != "pretend this is a screenshot" {
		t.Fatalf("the image reaching the model is %+v, want the %d bytes pasted as %s", last.Images[0], outcome.Bytes, outcome.Format())
	}
}

func TestDeletingAPastedImagesTokenDropsItFromTheRequestSentToTheModel(t *testing.T) {
	dir := scratchProject(t)
	model := &sendModel{queued: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "seen"}}}
	live := newAppSession(dir, func(runOpts) (appWire, error) {
		return wireOn(model), nil
	}, nil, time.Now, sessionResume{})

	board := screenshotBoard(t, live)
	board.Attach(1)()
	board.Attach(2)()

	var events eventLog
	task := "compare " + session.ImageToken(1)
	live.run(t.Context(), onTheSubscription, task, events.add)

	if len(model.requests) != 1 {
		t.Fatalf("the model was asked %d times, want one", len(model.requests))
	}
	last := model.requests[0].Messages[len(model.requests[0].Messages)-1]
	if len(last.Images) != 1 {
		t.Fatalf("the request carries %d images, want the one whose token survived", len(last.Images))
	}
	if string(last.Images[0].Data) != "pretend this is a screenshot" {
		t.Fatalf("the surviving image is %+v", last.Images[0])
	}
}

func TestAPasteInARepositoryWithNoRecordedSessionWorks(t *testing.T) {
	dir := scratchProject(t)
	live := newAppSession(dir, nil, nil, time.Now, sessionResume{})
	board := screenshotBoard(t, live)
	msg := board.Attach(1)()
	outcome, ok := msg.(paste.Outcome)
	if !ok || outcome.State != paste.Ready {
		t.Fatalf("pasting in %s with no session on disk returned %#v", dir, msg)
	}
	store, err := sessionstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.AttachmentDir(live.pendingID()), outcome.Name)); err != nil {
		t.Fatalf("the pasted image was not kept for the pending session: %v", err)
	}
}

func TestASendRecordsTheAttachmentEventInTheSessionBody(t *testing.T) {
	dir := scratchProject(t)
	model := &sendModel{queued: []llm.Decision{{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "seen"}}}
	live := newAppSession(dir, func(runOpts) (appWire, error) {
		return wireOn(model), nil
	}, nil, time.Now, sessionResume{})

	board := screenshotBoard(t, live)
	msg := board.Attach(1)()
	outcome := msg.(paste.Outcome)

	var events eventLog
	live.run(t.Context(), onTheSubscription, "look at what I pasted", events.add)

	store, err := sessionstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	body, err := store.Body(live.id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range body {
		if event.Kind != sessionstore.EventAttachment {
			continue
		}
		var attachment sessionstore.Attachment
		if err := json.Unmarshal(event.Body, &attachment); err != nil {
			t.Fatal(err)
		}
		if attachment.File == sessionstore.AttachmentPath(live.id, outcome.Name) && attachment.Bytes == outcome.Bytes && attachment.Format == "PNG" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the body of %s carries no attachment event naming %s, %d bytes, PNG", live.id, outcome.Name, outcome.Bytes)
	}

	raw, err := os.ReadFile(filepath.Join(store.Dir(live.id), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "pretend this is a screenshot") {
		t.Fatalf("the recorded body carries the image bytes:\n%s", raw)
	}
}

func TestAConversationThatOutgrowsItsBudgetAcrossSendsForks(t *testing.T) {
	dir := scratchProject(t)
	model := &sendModel{}
	send := stubbedTurn(dir, model)
	bulk := strings.Repeat("carry this sentence forward. ", 1500)
	forkedAt := 0
	for attempt := 1; attempt <= 5 && forkedAt == 0; attempt++ {
		model.queued = []llm.Decision{
			{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{writeNote("call-" + strconv.Itoa(attempt))}},
			{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "noted"},
		}
		var events eventLog
		send(t.Context(), onTheSubscription, bulk, events.add)
		for _, event := range events.all() {
			if event.Kind == tui.EventForkStart {
				forkedAt = attempt
			}
		}
	}
	t.Logf("the conversation forked on send %d, carrying %d characters per send", forkedAt, len(bulk))
	if forkedAt == 0 {
		t.Fatal("five sends of the same bulk never crossed the fork mark, so nothing grew across a send")
	}
	if forkedAt == 1 {
		t.Fatal("the first send forked on its own, so this proves nothing about growth across sends")
	}
}

func TestLiveASecondSendReadsTheCacheOfTheFirst(t *testing.T) {
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to spend subscription quota on two sends of one conversation")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	for name, sentence := range map[string]string{
		"loop.md":  "tofu keeps the loop in code and asks a typed model to classify state. ",
		"judge.md": "every decision tofu makes is a row in a ledger, and an uncalibrated backend asks. ",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat(sentence, 45)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	send := newAppSession(dir, openAppWire, nil, time.Now, sessionResume{}).run
	var events eventLog
	send(t.Context(), onTheSubscription, "read loop.md and judge.md and say in one sentence what each is about", events.add)
	send(t.Context(), onTheSubscription, "without calling any tool, repeat what judge.md was about", events.add)
	for _, event := range events.all() {
		if event.Kind == tui.EventFailure {
			t.Fatalf("the live run failed: %s", event.Text)
		}
	}

	store, err := sessionstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	listing, err := store.Listing()
	headers := listing.Sessions
	if err != nil || len(headers) != 1 {
		t.Fatalf("sessions %d unreadable %d err %v, want the two sends in one", len(headers), len(listing.Skipped), err)
	}
	body, err := store.Body(headers[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	sends, headerOnly, secondSend := 0, 0, 0
	for _, event := range body {
		if event.Kind == sessionstore.EventOutcome {
			sends++
			continue
		}
		if event.Kind != sessionstore.EventStep {
			continue
		}
		var step turn.StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			t.Fatal(err)
		}
		t.Logf("send %d step %d: prompt %d completion %d cache_read %d cache_write %d",
			sends+1, step.Index, step.PromptTokens, step.CompletionTokens, step.CacheReadTokens, step.CacheWriteTokens)
		switch {
		case sends == 0 && step.Index == 2:
			headerOnly = step.CacheReadTokens
		case sends == 1 && step.Index == 1:
			secondSend = step.CacheReadTokens
		}
	}
	if sends != 2 {
		t.Fatalf("the session holds %d sends, want two", sends)
	}
	if secondSend <= 0 {
		t.Fatalf("the second send read %d cached tokens", secondSend)
	}
	if secondSend <= headerOnly {
		t.Fatalf("the second send read %d cached tokens and the system and tool header alone is %d, so the first exchange was not part of the prefix",
			secondSend, headerOnly)
	}
}
