package library

import (
	"encoding/json"
	"testing"
	"time"

	"tofu/apps/cli/wiring"
	"tofu/internal/store/session"
)

func reachEvent(t *testing.T, kind session.EventKind, body any) session.Event {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return session.Event{Kind: kind, Turn: "turn-1", At: time.Unix(1, 0), Body: raw}
}

func TestReachCountsAnAgentFromEverySpawnThatNamesIt(t *testing.T) {
	spawn := func(args string) session.CallBody {
		return session.CallBody{Tool: "spawn", Args: json.RawMessage(args)}
	}
	tally := reachTally{}
	tally.item(wiring.LibraryWhatAgent, "go-dev")
	tally.read(session.Header{ID: "one"}, []session.Event{
		reachEvent(t, session.EventToolCall, spawn(`{"agent":"rust-designer","task":"a"}`)),
		reachEvent(t, session.EventToolCall, spawn(`{"agent":"rust-designer","task":"b"}`)),
		reachEvent(t, session.EventToolCall, spawn(`{"task":"no agent named"}`)),
		reachEvent(t, session.EventToolCall, spawn(`{"agent":""}`)),
		reachEvent(t, session.EventToolCall, spawn(`"agent rust-designer"`)),
		reachEvent(t, session.EventPrompt, session.PromptBody{Parts: []session.PromptPart{{Kind: "identity", Agent: "rust-designer", File: "x.md"}}}),
	})
	tally.read(session.Header{ID: "two"}, []session.Event{
		reachEvent(t, session.EventToolCall, spawn(`{"agent":"rust-designer"}`)),
		reachEvent(t, session.EventToolCall, spawn(`{"agent":"go-dev"}`)),
		reachEvent(t, session.EventToolCall, session.CallBody{Tool: "read", Args: json.RawMessage(`{"agent":"qa"}`)}),
	})
	for _, want := range []struct {
		kind, name     string
		reached, calls int
	}{
		{wiring.LibraryWhatAgent, "rust-designer", 2, 3},
		{wiring.LibraryWhatAgent, "go-dev", 1, 1},
		{wiring.LibraryWhatAgent, wiring.LibraryUnnamedAgent, 1, 2},
		{wiring.LibraryWhatAgent, "qa", 0, 0},
		{whatTool, "spawn", 2, 7},
	} {
		row := tally.item(want.kind, want.name)
		if len(row.reachedIn) != want.reached || row.Calls != want.calls {
			t.Errorf("%s %s: reached %d calls %d, want %d and %d", want.kind, want.name, len(row.reachedIn), row.Calls, want.reached, want.calls)
		}
	}
}
