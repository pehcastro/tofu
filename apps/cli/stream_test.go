package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"tofu/internal/models/llm/core"
	sessionstore "tofu/internal/store/session"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/testkit/cli"
	"tofu/internal/ui/tui/app"
	"tofu/internal/ui/tui/frame/frame"
)

type streamingModel struct {
	deltas  []string
	content string
	cancel  context.CancelFunc
}

func (m *streamingModel) Ask(ctx context.Context, request core.Request) (core.Decision, error) {
	for _, delta := range m.deltas {
		if request.OnDelta != nil {
			request.OnDelta(delta)
		}
	}
	if m.cancel != nil {
		m.cancel()
		return core.Decision{}, context.Canceled
	}
	return core.Decision{Build: "stub-model", Outcome: core.OutcomeMessage, Content: m.content}, nil
}

func TestAStreamedResponseDrawsEveryDeltaAndNoDuplicateFinalText(t *testing.T) {
	dir := cli.ScratchProject(t)
	model := &streamingModel{deltas: []string{"the gate reads ", "toolgate.go "}, content: "the gate reads toolgate.go"}
	driver := driveApp(t)
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "explain the gate", driver.emit)

	deltas := driver.of(app.EventTextDelta)
	if len(deltas) != 2 {
		t.Fatalf("%d deltas reached the interface, want 2", len(deltas))
	}
	if deltas[0].Text != "the gate reads " || deltas[1].Text != "toolgate.go " {
		t.Fatalf("deltas read %+v", deltas)
	}
	if texts := driver.of(app.EventText); len(texts) != 0 {
		t.Fatalf("a streamed answer also drew %d whole EventText entries, want 0: %+v", len(texts), texts)
	}
}

func TestAWireThatDoesNotStreamDrawsNoDeltaAndBehavesAsBefore(t *testing.T) {
	dir := cli.ScratchProject(t)
	model := &sendModel{queued: []core.Decision{{Build: "stub-model", Outcome: core.OutcomeMessage, Content: "the whole answer at once"}}}
	driver := driveApp(t)
	stubbedTurn(dir, model)(t.Context(), onTheSubscription, "explain the gate", driver.emit)

	if deltas := driver.of(app.EventTextDelta); len(deltas) != 0 {
		t.Fatalf("a model that never called OnDelta still drew %d deltas", len(deltas))
	}
	texts := driver.of(app.EventText)
	if len(texts) != 1 || texts[0].Text != "the whole answer at once" {
		t.Fatalf("the unstreamed answer arrived as %+v, want the whole text in one EventText", texts)
	}
}

func TestACancelledStreamSealsTheEntryItWasWriting(t *testing.T) {
	dir := cli.ScratchProject(t)
	ctx, cancel := context.WithCancel(t.Context())
	model := &streamingModel{deltas: []string{"The **gate** reads ", "`toolgate.go` before the policy."}, cancel: cancel}
	driver := driveApp(t)
	stubbedTurn(dir, model)(ctx, onTheSubscription, "explain the gate", driver.emit)

	if deltas := driver.of(app.EventTextDelta); len(deltas) != 2 {
		t.Fatalf("%d deltas reached the interface before the cancel, want 2", len(deltas))
	}
	if done := driver.of(app.EventDone); len(done) != 1 {
		t.Fatalf("a cancelled stream ended with %d done events, want 1", len(done))
	}
	screen := driver.view()
	if strings.Contains(screen, "**gate**") || strings.Contains(screen, "`toolgate.go`") {
		t.Errorf("the entry still shows raw markdown after the turn ended, so it was never sealed:\n%s", screen)
	}
}

func TestACacheHitCarriesTheReadSeparatelyFromTheFreshInput(t *testing.T) {
	cached := cli.ScratchProject(t)
	cachedModel := &sendModel{queued: []core.Decision{{
		Build: "stub-model", Outcome: core.OutcomeMessage, Content: "the answer",
		Usage: core.Usage{InputTokens: 400}, CacheReadTokens: 9603,
	}}}
	detailed, err := settingspkg.OpenFor(cached)
	if err != nil {
		t.Fatal(err)
	}
	if err := detailed.SetText(settingspkg.Project, settingspkg.StatusBar, string(frame.StatusDetailed)); err != nil {
		t.Fatal(err)
	}
	cachedDriver := driveAppOn(t, detailed)
	stubbedTurn(cached, cachedModel)(t.Context(), onTheSubscription, "explain the gate", cachedDriver.emit)

	fresh := cli.ScratchProject(t)
	freshModel := &sendModel{queued: []core.Decision{{
		Build: "stub-model", Outcome: core.OutcomeMessage, Content: "the answer",
		Usage: core.Usage{InputTokens: 400},
	}}}
	freshDriver := driveAppOn(t, detailed)
	stubbedTurn(fresh, freshModel)(t.Context(), onTheSubscription, "explain the gate", freshDriver.emit)

	stats := cachedDriver.of(app.EventStats)
	if len(stats) == 0 || stats[len(stats)-1].CacheRead != 9603 {
		t.Fatalf("the stats event does not carry the cached read: %+v", stats)
	}
	wide := tea.WindowSizeMsg{Width: 160, Height: 24}
	cachedFrame := cachedDriver.view(wide)
	freshFrame := freshDriver.view(wide)
	if !strings.Contains(cachedFrame, "9k cached") {
		t.Fatalf("a cache hit does not show the cached read beside the fresh input\n%s", cachedFrame)
	}
	if strings.Contains(freshFrame, "cached") {
		t.Fatalf("a turn with no cache hit still shows a cache mark\n%s", freshFrame)
	}
	if cachedFrame == freshFrame {
		t.Fatalf("a cache hit and a cache miss render the same frame")
	}
}

func recordedEventKinds(t *testing.T, dir string) []sessionstore.EventKind {
	t.Helper()
	matches, err := filepath.Glob(projectSessions(t, dir).Dir("*-*-*-*-*"))
	if err != nil {
		t.Fatalf("globbing for the session directory: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("found %d session directories, want 1: %v", len(matches), matches)
	}
	raw, err := os.ReadFile(filepath.Join(matches[0], "events.jsonl"))
	if err != nil {
		t.Fatalf("reading the recorded body: %v", err)
	}
	var kinds []sessionstore.EventKind
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var event sessionstore.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("a recorded line is not an event: %v", err)
		}
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

func TestAStreamedTurnRecordsTheSameEventKindsAsAnUnstreamedOne(t *testing.T) {
	without := cli.ScratchProject(t)
	stubbedTurn(without, &sendModel{queued: []core.Decision{{Build: "stub-model", Outcome: core.OutcomeMessage, Content: "the answer"}}})(
		t.Context(), onTheSubscription, "explain the gate", func(app.Event) {})
	withoutKinds := recordedEventKinds(t, without)

	streamed := cli.ScratchProject(t)
	stubbedTurn(streamed, &streamingModel{deltas: []string{"the ", "answer"}, content: "the answer"})(
		t.Context(), onTheSubscription, "explain the gate", func(app.Event) {})
	streamedKinds := recordedEventKinds(t, streamed)

	if len(withoutKinds) != len(streamedKinds) {
		t.Fatalf("the unstreamed body carries %d events and the streamed one %d: %v vs %v",
			len(withoutKinds), len(streamedKinds), withoutKinds, streamedKinds)
	}
	for index := range withoutKinds {
		if withoutKinds[index] != streamedKinds[index] {
			t.Fatalf("event %d is %q without streaming and %q with it", index, withoutKinds[index], streamedKinds[index])
		}
	}
	for _, kind := range streamedKinds {
		if kind == "delta" || kind == "text_delta" {
			t.Fatalf("a delta event reached the recorded body: %v", streamedKinds)
		}
	}
}
