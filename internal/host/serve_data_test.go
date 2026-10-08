package host

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/memory"
	"tofu/internal/turn"
)

func TestServeDataQueriesAreTypedAndMemoryReachesTheLead(t *testing.T) {
	verbs := map[string]VerbResult{
		"rules list":     {OK: true, Data: json.RawMessage(`{"origin":"shipped","rules":[{"id":"r1","kind":"check","origin":"shipped"}]}`)},
		"settings":       {OK: true, Data: json.RawMessage(`{"global_file":"g","project_file":"p","settings":[],"surprise":1}`)},
		"update --check": {Data: json.RawMessage(`null`), Problems: []Problem{{What: "no release published yet"}}},
		"models reload": {Data: json.RawMessage(`{"context_windows":0,"table":"t","sources":[{"source":"claude-sub","state":"failed","served":0,"changes":[],"failure":"list refused"}],"versions":{}}`),
			Problems: []Problem{{What: "claude-sub: list refused"}}},
		"models": {OK: true, Data: json.RawMessage(`{"subscriptions":[],"defaults":[],"usable":0,"table":"","windowed":0,"published_windows":0,"models":[]}`)},
	}
	var ran []string
	verb := func(args []string) (VerbResult, error) {
		ran = append(ran, strings.Join(args, " "))
		return verbs[strings.Join(args, " ")], nil
	}
	c, h, _ := serving(t, nil, ServeConfig{Verb: verb, Wires: func() []string { return []string{"claude-sub"} }})
	c.ask("1", "initialize", `{"client":"scratch"}`)
	var started InitializeResult
	c.answer("1", &started)
	for _, family := range []string{"memory", "reload", "setup", "hooks", "docs", "typed"} {
		if !slices.Contains(started.Capabilities, family) {
			t.Errorf("initialize names %v and not %q", started.Capabilities, family)
		}
	}

	c.ask("2", "query.rules", `{}`)
	var rules map[string]json.RawMessage
	c.answer("2", &rules)
	if _, enveloped := rules["data"]; enveloped || string(rules["origin"]) != `"shipped"` {
		t.Errorf("query.rules answered %v, want the rules report itself rather than a verb envelope", rules)
	}

	c.ask("3", "query.settings", `{}`)
	if drifted := c.until(func(line wireLine) bool { return string(line.ID) == `"3"` }, "the answer to 3"); drifted.Error == nil || !strings.Contains(drifted.Error.Message, "surprise") {
		t.Errorf("a settings report carrying a field the wire does not name answered %s, want a refusal naming the field", drifted.Result)
	}

	c.ask("4", "query.update", `{}`)
	if failed := c.until(func(line wireLine) bool { return string(line.ID) == `"4"` }, "the answer to 4"); failed.Error == nil || !strings.Contains(failed.Error.Message, "no release published yet") {
		t.Errorf("a verb that failed with no report answered %s, want its problem as the error", failed.Result)
	}

	c.ask("5", "models.reload", `{}`)
	var reloaded ModelReload
	c.answer("5", &reloaded)
	if len(reloaded.Sources) != 1 || reloaded.Sources[0].Failure != "list refused" {
		t.Errorf("models.reload with a failing source answered %+v, want the report naming the failure", reloaded)
	}

	c.ask("6", "query.models", `{}`)
	var listed ModelsQuery
	c.answer("6", &listed)
	if !slices.Equal(listed.Wires, []string{"claude-sub"}) || listed.Models == nil {
		t.Errorf("query.models answered %+v, want the models report and the signed-in wires", listed)
	}

	c.ask("7", "memory.add", `{"text":"prefers tabs","scope":"global"}`)
	var added memory.Entry
	c.answer("7", &added)
	heard := strings.Join(h.inbox.Take(), "\n")
	if added.Scope != memory.Global || added.Kind != memory.KindPerson || !strings.Contains(heard, "prefers tabs") {
		t.Errorf("memory.add kept %+v and the lead heard %q, want a global person entry the lead hears", added, heard)
	}
	c.ask("8", "memory.edit", `{"scope":"global","id":"`+added.ID+`","text":"prefers spaces"}`)
	var edited memory.Entry
	c.answer("8", &edited)
	if edited.ID != added.ID || edited.Text != "prefers spaces" {
		t.Errorf("memory.edit kept %+v, want %s with the new text", edited, added.ID)
	}
	c.ask("9", "memory.remove", `{"scope":"project","id":"`+added.ID+`"}`)
	if wrong := c.until(func(line wireLine) bool { return string(line.ID) == `"9"` }, "the answer to 9"); wrong.Error == nil {
		t.Errorf("memory.remove of a global entry from the project scope answered %s", wrong.Result)
	}
	c.ask("10", "memory.remove", `{"scope":"global","id":"`+added.ID+`"}`)
	var removed memory.Entry
	c.answer("10", &removed)
	shelves, err := memory.Open(h.dir)
	if err != nil || removed.ID != added.ID || len(shelves.Global.Entries) != 0 {
		t.Errorf("memory.remove answered %+v and the shelf still holds %v (%v)", removed, shelves.Global.Entries, err)
	}

	c.ask("11", "query.docs", `{"topik":"serve"}`)
	if unknown := c.until(func(line wireLine) bool { return string(line.ID) == `"11"` }, "the answer to 11"); unknown.Error == nil {
		t.Errorf("query.docs took a param it does not have")
	}

	c.ask("12", "rules.add", `{"id":"r1","text":"say so","reason":"why","global":true}`)
	c.until(func(line wireLine) bool { return string(line.ID) == `"12"` }, "the answer to 12")
	c.ask("13", "query.context", `{}`)
	c.until(func(line wireLine) bool { return string(line.ID) == `"13"` }, "the answer to 13")
	c.ask("14", "agents.set", `{"name":"go-dev","model":"claude-sub/opus"}`)
	c.until(func(line wireLine) bool { return string(line.ID) == `"14"` }, "the answer to 14")
	for _, want := range []string{"rules add --global --reason why r1 say so", "context", "agents set go-dev claude-sub/opus"} {
		if !slices.Contains(ran, want) {
			t.Errorf("no verb ran as %q; ran %q", want, ran)
		}
	}

	schema, err := Schema()
	if err != nil || !strings.Contains(string(schema), `"#/$defs/RuleListReport"`) || !strings.Contains(string(schema), `"memory.Entry"`) {
		t.Errorf("the schema does not name the typed reports: %v", err)
	}
}

func TestServeDataRememberAnswersCarryTheScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, _ := New(Config{})
	s := &server{ServeConfig: ServeConfig{Host: h}, box: newOutbox(), pending: map[string]ApprovalRequest{},
		items: items{tools: map[string]openTool{}, agents: map[string]SubAgentRow{}}}
	awaited := make(chan string, 1)
	person := awaitPerson(func(event Event) {
		s.publish(event)
		if event.Kind == EventAwaitPerson {
			awaited <- event.ID
		}
	}, h.asks)
	ask := func(tool, args string) (chan turn.PersonAnswer, string) {
		got := make(chan turn.PersonAnswer, 1)
		go func() {
			answer, _ := person(context.Background(), turn.GateRequest{Tool: tool, Args: json.RawMessage(args)}, turn.GateDecision{})
			got <- answer
		}()
		return got, <-awaited
	}
	remembered := `{"statement":"prefers tabs","scope":"project","said":"I prefer tabs"}`
	for decision, want := range map[string]turn.PersonAnswer{"remember_project": turn.PersonAllowedOnce, "remember_global": turn.PersonAlwaysHere} {
		got, id := ask(turn.RememberToolName, remembered)
		s.answered(json.RawMessage(`"`+id+`"`), json.RawMessage(`{"decision":"`+decision+`"}`))
		if answer := <-got; answer != want {
			t.Errorf("%s on a remember ask answered %v, want %v", decision, answer, want)
		}
	}
	got, id := ask("bash", `{"command":"git push --force"}`)
	s.answered(json.RawMessage(`"`+id+`"`), json.RawMessage(`{"decision":"remember_global"}`))
	select {
	case answer := <-got:
		t.Fatalf("remember_global on a bash ask answered it %v", answer)
	case <-time.After(100 * time.Millisecond):
	}
	s.answered(json.RawMessage(`"`+id+`"`), json.RawMessage(`{"decision":"reject_once"}`))
	if answer := <-got; answer != turn.PersonDenied {
		t.Errorf("reject_once after an ignored remember answer gave %v", answer)
	}
}
