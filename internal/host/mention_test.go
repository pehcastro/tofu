package host

import (
	"context"
	"encoding/json"
	"testing"

	"tofu/internal/turn/tools"
)

func TestTurnSendKeepsAMentionWhereItWasAndEveryLineCarriesTheTokenForItsItem(t *testing.T) {
	tasks := make(chan string, 1)
	play := func(_ context.Context, _ Pick, task string, live Live) {
		tasks <- task
		live.Emit(Event{Kind: EventText, Text: "it changed the port"})
	}
	c, _, _ := serving(t, play, ServeConfig{})
	c.ask("1", "initialize", `{"client":"scratch"}`)
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	c.answer("2", &opened)

	c.ask("3", "turn.send", `{"session":"`+opened.Session+`","text":"what did [quote#abc123] change","mentions":["[quote#abc123]","[quote#def456]","[quote#def456]","notes/a.md"]}`)
	if task, want := <-tasks, "what did [quote#abc123] change [quote#def456] @notes/a.md"; task != want {
		t.Errorf("the lead read %q, want %q", task, want)
	}
	lines := 0
	c.until(func(line wireLine) bool {
		var about Identity
		if json.Unmarshal(line.Params, &about) == nil && about.Item != "" {
			lines++
			if about.Ref != tools.QuoteRef(about.Item) {
				t.Errorf("%s carries ref %q for item %q, want %q", line.Method, about.Ref, about.Item, tools.QuoteRef(about.Item))
			}
		}
		return line.Method == "turn.completed"
	}, "turn.completed")
	if lines == 0 {
		t.Fatal("no line carried an item, so no ref was checked")
	}
}

func TestMentionResolveTypesAMadeUpTokenAndRefusesAnEmptyOne(t *testing.T) {
	play := func(_ context.Context, _ Pick, _ string, live Live) {
		live.Emit(Event{Kind: EventText, Text: "done"})
	}
	c, _, _ := serving(t, play, ServeConfig{})
	c.ask("1", "initialize", `{"client":"scratch"}`)
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	c.answer("2", &opened)
	c.ask("3", "turn.send", `{"session":"`+opened.Session+`","text":"hello"}`)
	c.until(func(line wireLine) bool { return line.Method == "turn.completed" }, "turn.completed")

	c.ask("4", "mention.resolve", `{"ref":"[quote#000000]"}`)
	var resolved MentionResolved
	c.answer("4", &resolved)
	if resolved.Outcome != MentionNotFound || resolved.Item != "" {
		t.Errorf("a made-up token resolved to %+v, want not_found and no item", resolved)
	}
	c.ask("5", "mention.resolve", `{"ref":"[quote#]"}`)
	if refused := c.until(func(line wireLine) bool { return string(line.ID) == `"5"` }, "the answer to 5"); refused.Error == nil || refused.Error.Code != CodeBadParams {
		t.Errorf("an empty token answered %s %+v, want a bad params refusal", refused.Result, refused.Error)
	}
}
