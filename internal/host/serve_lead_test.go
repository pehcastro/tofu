package host

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/llm"
	"tofu/internal/session"
)

type leadTurn struct {
	task      string
	leadOnly  bool
	steering  []string
	ranBefore string
}

func TestServeStopsTheLeadUnsteersRunsCommandsCompactsAndPagesHistory(t *testing.T) {
	var h *Host
	turns := make(chan leadTurn, 4)
	play := func(ctx context.Context, pick Pick, task string, live Live) {
		seen := leadTurn{task: task, ranBefore: h.takeRan()}
		if strings.Contains(task, "hold") {
			select {
			case <-live.LeadStop:
				seen.leadOnly = true
			case <-ctx.Done():
			}
			for queued := true; queued; {
				select {
				case text := <-live.Steering:
					seen.steering = append(seen.steering, text)
				default:
					queued = false
				}
			}
		}
		turns <- seen
	}
	var asked []LedgerParams
	ledger := func(p LedgerParams) (LedgerReport, error) {
		asked = append(asked, p)
		return LedgerReport{Rows: []LedgerRow{}}, nil
	}
	run := func(ctx context.Context, command string) (string, bool) {
		if command == "sleep 30" {
			<-ctx.Done()
			return "", true
		}
		return strings.TrimPrefix(command, "echo ") + "\n", false
	}
	compacted := Compaction{Into: "turn-compacted", Results: 2, TokensBefore: 900, TokensAfter: 300}
	resumed := session.Header{ID: session.NewEventID(), At: time.Now()}
	carry := func(handle string) (Carry, error) {
		return Carry{Session: resumed.ID, Tasks: []string{"first task", "second task"}, Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "first task"}, {Role: llm.RoleAssistant, Content: "first answer"},
			{Role: llm.RoleUser, Content: "second task"}, {Role: llm.RoleAssistant, Content: "second answer"},
		}}, nil
	}
	c, live, project := serving(t, play, ServeConfig{Ledger: ledger, Run: run, Carry: carry, Compact: func() (Compaction, error) { return compacted, nil }})
	h = live
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	recordSession(t, store, resumed)

	c.ask("1", "initialize", `{"client":"scratch"}`)
	var started InitializeResult
	c.answer("1", &started)
	for _, family := range []string{"lead", "unsteer", "run", "compact", "history", "ledger"} {
		if !slices.Contains(started.Capabilities, family) {
			t.Errorf("initialize names %v and not %q", started.Capabilities, family)
		}
	}

	c.ask("2", "session.open", `{"session":"`+resumed.ID+`","replay":3}`)
	var replayed []wireLine
	c.until(func(line wireLine) bool {
		if strings.HasPrefix(line.Method, "message.") || line.Method == "session.updated" {
			replayed = append(replayed, line)
		}
		return string(line.ID) == `"2"`
	}, "the answer to session.open")
	if len(replayed) != 3 || replayed[0].Method != "message.user" || replayed[2].Method != "message.completed" {
		t.Fatalf("session.open replay 3 sent %d lines %v, want the last three: the second task and its answer", len(replayed), methodsOf(replayed))
	}

	pages := []SessionHistory{}
	for id, before := range map[string]string{"3a": ``, "3b": `,"before":4`, "3c": `,"before":1`, "3d": `,"before":99`} {
		c.ask(id, "session.history", `{"session":"`+resumed.ID+`","limit":3`+before+`}`)
		var page SessionHistory
		c.answer(id, &page)
		pages = append(pages, page)
	}
	slices.SortFunc(pages, func(a, b SessionHistory) int { return b.First - a.First })
	if pages[0].First != 4 || pages[1].First != 4 || pages[2].First != 1 || pages[3].First != 0 {
		t.Fatalf("history pages start at %d %d %d %d, want 4 4 1 0", pages[0].First, pages[1].First, pages[2].First, pages[3].First)
	}
	if pages[0].Total != 7 || len(pages[0].Lines) != 3 || len(pages[2].Lines) != 3 || len(pages[3].Lines) != 1 || pages[3].Lines[0].Method != "session.updated" {
		t.Fatalf("history pages = %+v, want seven lines paged 3, 3, 1 from session.updated", pages)
	}
	var lastPage, openReplay Text
	_ = json.Unmarshal(mustJSON(t, pages[0].Lines[2].Params), &lastPage)
	_ = json.Unmarshal(replayed[2].Params, &openReplay)
	if lastPage.Item == "" || lastPage.Item != openReplay.Item || lastPage.Text != "second answer" {
		t.Errorf("the newest history line names item %q %q, and session.open replayed it as %q", lastPage.Item, lastPage.Text, openReplay.Item)
	}

	c.ask("4", "turn.send", `{"session":"`+resumed.ID+`","text":"hold here"}`)
	var sent TurnResult
	c.answer("4", &sent)
	c.ask("5", "shell.run", `{"command":"echo early"}`)
	if refused := c.until(func(line wireLine) bool { return string(line.ID) == `"5"` }, "the answer to 5"); refused.Error == nil {
		t.Error("shell.run during a turn was accepted")
	}
	c.ask("6", "session.compact", `{}`)
	if refused := c.until(func(line wireLine) bool { return string(line.ID) == `"6"` }, "the answer to 6"); refused.Error == nil {
		t.Error("session.compact during a turn was accepted")
	}
	for id, text := range map[string]string{"7a": "keep this", "7b": "take this back"} {
		c.ask(id, "turn.steer", `{"session":"`+resumed.ID+`","expectedTurnId":"`+sent.Turn+`","text":"`+text+`"}`)
		c.answer(id, &sent)
	}
	var removed, absent UnsteerResult
	c.ask("8", "turn.unsteer", `{"text":"take this back"}`)
	c.answer("8", &removed)
	c.ask("9", "turn.unsteer", `{"text":"never sent"}`)
	c.answer("9", &absent)
	if !removed.Removed || absent.Removed {
		t.Errorf("turn.unsteer answered %v for a queued steer and %v for one never sent", removed.Removed, absent.Removed)
	}
	c.ask("10", "turn.stop", `{"lead":true}`)
	var stopped Ack
	c.answer("10", &stopped)
	held := <-turns
	if !stopped.OK || !held.leadOnly {
		t.Errorf("turn.stop lead answered %v and the turn saw a lead stop %v, want the lead alone stopped", stopped.OK, held.leadOnly)
	}
	if !slices.Equal(held.steering, []string{"keep this"}) {
		t.Errorf("the turn read the steering %q, want only the one not taken back", held.steering)
	}
	c.until(func(line wireLine) bool { return line.Method == "turn.completed" }, "turn.completed")

	c.ask("11", "shell.run", `{"command":"sleep 30"}`)
	c.ask("12", "session.state", `{}`)
	var state SessionState
	c.answer("12", &state)
	c.ask("13", "turn.stop", `{}`)
	c.answer("13", &stopped)
	var slept, echoed ShellRunResult
	c.answer("11", &slept)
	if !stopped.OK || !slept.Stopped {
		t.Errorf("turn.stop during a running shell.run answered %v and the command reported stopped %v", stopped.OK, slept.Stopped)
	}
	c.ask("14", "shell.run", `{"command":"echo hi"}`)
	c.answer("14", &echoed)
	if echoed.Output != "hi\n" || echoed.Stopped {
		t.Errorf("shell.run echo hi = %+v", echoed)
	}
	c.ask("15", "turn.send", `{"session":"`+resumed.ID+`","text":"next"}`)
	c.answer("15", &sent)
	if next := <-turns; !strings.Contains(next.ranBefore, "$ echo hi\nhi") || strings.Contains(next.ranBefore, "sleep 30") {
		t.Errorf("the turn after shell.run carried %q, want the command that ran and not the one stopped", next.ranBefore)
	}
	c.until(func(line wireLine) bool { return line.Method == "turn.completed" }, "turn.completed")

	c.ask("16", "session.compact", `{}`)
	var updated SessionUpdated
	c.until(func(line wireLine) bool {
		return line.Method == "session.updated" && json.Unmarshal(line.Params, &updated) == nil && updated.Session == compacted.Into
	}, "session.updated naming the compacted session")
	var result Compaction
	c.answer("16", &result)
	if result != compacted {
		t.Errorf("session.compact = %+v, want %+v", result, compacted)
	}

	var report LedgerReport
	c.ask("17", "query.ledger", `{}`)
	c.answer("17", &report)
	c.ask("18", "query.ledger", `{"last":3,"point":"tool_gate"}`)
	c.answer("18", &report)
	if report.Rows == nil || !slices.Contains(asked, LedgerParams{}) || !slices.Contains(asked, LedgerParams{Last: 3, Point: "tool_gate"}) {
		t.Errorf("query.ledger asked the ledger %+v and answered %+v", asked, report)
	}
	schema, err := Schema()
	if err != nil || !strings.Contains(string(schema), `"#/$defs/ledger.Reason"`) {
		t.Errorf("the schema does not tell the ledger's reason from a decision's: %v", err)
	}
}

func methodsOf(lines []wireLine) []string {
	var methods []string
	for _, line := range lines {
		methods = append(methods, line.Method)
	}
	return methods
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
