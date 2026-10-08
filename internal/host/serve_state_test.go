package host

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type playedTurn struct {
	pick   Pick
	task   string
	images int
}

func TestServeListsSessionsReportsStateAndKeepsTheSettingsItWasGiven(t *testing.T) {
	var h *Host
	played := make(chan playedTurn, 4)
	release := make(chan struct{})
	play := func(ctx context.Context, pick Pick, task string, live Live) {
		images, _ := h.takePendingImages(task)
		played <- playedTurn{pick: pick, task: task, images: len(images)}
		if strings.Contains(task, "ask me") {
			live.Emit(Event{Kind: EventAwaitPerson, ID: "ask-1", Tool: "bash", Text: "rm -rf build", Args: json.RawMessage(`{"command":"rm -rf build"}`)})
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}
	lastAt := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	listed := func(open string) (SessionList, error) {
		return SessionList{Head: open, Sessions: []SessionRow{
			{ID: open, Name: "gate", Handle: "gate", LastAt: lastAt, Open: true, Wire: "anthropic"},
			{ID: "turn-other", Name: "docs", Handle: "docs", Task: "write the DOCS", HeldBy: &SessionHolder{PID: 42, Since: lastAt}},
		}}, nil
	}
	c, live, project := serving(t, play, ServeConfig{Sessions: listed, Wires: func() []string { return []string{"anthropic", "codex"} },
		Sources: map[string]string{"anthropic": "claude-sub", "codex": "codex-sub", "meta": "meta"}})
	h = live

	c.ask("1", "initialize", `{"client":"scratch"}`)
	var started InitializeResult
	c.answer("1", &started)
	for _, family := range []string{"list", "state", "set", "images", "rename", "cron"} {
		if !slices.Contains(started.Capabilities, family) {
			t.Errorf("initialize names %v and not %q, so a desk cannot tell this serve serves it", started.Capabilities, family)
		}
	}
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	c.answer("2", &opened)

	c.ask("3", "session.list", `{}`)
	var all SessionList
	c.answer("3", &all)
	if len(all.Sessions) != 2 || !all.Sessions[0].Open || all.Sessions[0].Running || !all.Sessions[0].LastAt.Equal(lastAt) || all.Sessions[1].HeldBy == nil {
		t.Fatalf("session.list = %+v, want two typed rows, the open one not running and carrying lastAt, the other held", all)
	}
	if all.Sessions[0].Wire != "claude-sub" {
		t.Errorf("session.list names the wire %q, the internal name, rather than claude-sub, the source a person sees", all.Sessions[0].Wire)
	}
	c.ask("4", "session.list", `{"search":"docs"}`)
	var found SessionList
	c.answer("4", &found)
	if len(found.Sessions) != 1 || found.Sessions[0].ID != "turn-other" {
		t.Errorf("session.list search docs = %+v, want only the session whose task says DOCS", found.Sessions)
	}
	c.ask("5", "session.list", `{"limit":1}`)
	var one SessionList
	c.answer("5", &one)
	if len(one.Sessions) != 1 {
		t.Errorf("session.list limit 1 gave %d rows", len(one.Sessions))
	}

	c.ask("6", "session.set", `{"asking":"ask","wire":"codex-sub","model":"gpt-5.6","effort":"high"}`)
	var settings SessionSettings
	c.until(func(line wireLine) bool {
		return line.Method == "session.settings" && json.Unmarshal(line.Params, &settings) == nil
	}, "session.settings after session.set")
	if settings.Asking != AskingAsk || settings.Pick != (ModelPick{Wire: "codex-sub", Model: "gpt-5.6", Effort: "high"}) {
		t.Errorf("session.settings = %+v, want asking ask on codex-sub gpt-5.6 high", settings)
	}
	var ack Ack
	c.answer("6", &ack)
	for id, params := range map[string]string{
		"7a": `{"wire":"meta"}`,
		"7e": `{"wire":"anthropic"}`,
		"7f": `{"wire":"codex"}`,
		"7b": `{"asking":"maybe"}`,
		"7c": `{"effort":"huge"}`,
		"7d": `{"model":"other","colour":"red"}`,
	} {
		c.ask(id, "session.set", params)
		if refused := c.until(func(line wireLine) bool { return string(line.ID) == `"`+id+`"` }, "the answer to "+id); refused.Error == nil {
			t.Errorf("session.set %s was accepted", params)
		}
	}

	picture := filepath.Join(t.TempDir(), "flag.png")
	notes := filepath.Join(t.TempDir(), "notes.txt")
	for path, body := range map[string]string{picture: "png bytes", notes: "text"} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for id, path := range map[string]string{"8a": filepath.Join(project, "nowhere.png"), "8b": notes} {
		c.ask(id, "turn.send", `{"session":"`+opened.Session+`","text":"look","images":[{"path":`+quoted(path)+`}]}`)
		if refused := c.until(func(line wireLine) bool { return string(line.ID) == `"`+id+`"` }, "the answer to "+id); refused.Error == nil {
			t.Errorf("turn.send with image %s was accepted", path)
		}
	}
	c.ask("8c", "turn.send", `{"session":"`+opened.Session+`","text":"look","wire":"meta"}`)
	if refused := c.until(func(line wireLine) bool { return string(line.ID) == `"8c"` }, "the answer to 8c"); refused.Error == nil {
		t.Error("turn.send on a wire nobody signed in to was accepted, so it would run on the default")
	}

	c.ask("9", "turn.send", `{"session":"`+opened.Session+`","text":"ask me about this","images":[{"path":`+quoted(picture)+`}]}`)
	var sent TurnResult
	c.answer("9", &sent)
	first := <-played
	if first.pick.Wire != "codex" || first.pick.Model != "gpt-5.6" || first.pick.Effort != "high" {
		t.Errorf("the turn after session.set ran on %+v, want the internal wire codex, gpt-5.6, high", first.pick)
	}
	if !strings.Contains(first.task, ImageToken(1)) || first.images != 1 {
		t.Errorf("the turn read %q with %d images, want the image token and the image", first.task, first.images)
	}
	c.until(func(line wireLine) bool { return line.Method == ApprovalMethod }, "the approval request")

	c.ask("10", "session.state", `{}`)
	var during SessionState
	c.answer("10", &during)
	if !during.Running || during.Turn == nil || during.Turn.ID != sent.Turn || !strings.HasPrefix(during.Turn.Task, "ask me") || during.Asking != AskingAsk || during.Pick != (ModelPick{Wire: "codex-sub", Model: "gpt-5.6", Effort: "high"}) {
		t.Errorf("session.state during the turn = %+v, want it running %s on gpt-5.6 asking", during, sent.Turn)
	}
	if len(during.Approvals) != 1 || during.Approvals[0].Approval != "ask-1" || during.Approvals[0].Target != "rm -rf build" {
		t.Errorf("session.state approvals = %+v, want the waiting ask-1", during.Approvals)
	}
	c.ask("11", "session.list", `{}`)
	var busy SessionList
	c.answer("11", &busy)
	if !busy.Sessions[0].Running {
		t.Errorf("session.list during a turn says the open session is not running: %+v", busy.Sessions[0])
	}

	if _, err := io.WriteString(c.in, `{"jsonrpc":"2.0","id":"ask-1","result":{"decision":"allow_once"}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	c.until(func(line wireLine) bool { return line.Method == "approval.resolved" }, "approval.resolved")
	close(release)
	var row SessionListed
	c.until(func(line wireLine) bool {
		return line.Method == "session.listed" && json.Unmarshal(line.Params, &row) == nil && !row.Running
	}, "session.listed saying the turn ended")
	if row.ID != opened.Session || !row.Open {
		t.Errorf("session.listed after the turn = %+v, want the open session, no longer running", row.SessionRow)
	}

	c.ask("12", "session.state", `{}`)
	var after SessionState
	c.answer("12", &after)
	if after.Running || after.Turn != nil || len(after.Approvals) != 0 || after.Approvals == nil || after.Agents == nil || after.Shells == nil {
		t.Errorf("session.state after the turn = %+v, want idle with empty lists rather than null", after)
	}

	c.ask("13", "turn.send", `{"session":"`+opened.Session+`","text":"again"}`)
	c.answer("13", &sent)
	if again := <-played; again.pick.Model != "gpt-5.6" || again.pick.Wire != "codex" {
		t.Errorf("a turn.send naming no model ran on %+v rather than the model session.set chose", again.pick)
	}
}

func quoted(text string) string {
	raw, _ := json.Marshal(text)
	return string(raw)
}
