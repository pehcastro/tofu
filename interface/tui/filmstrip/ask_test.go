package filmstrip

import (
	"strings"
	"testing"
	"time"

	"tofu/interface/tui"
	"tofu/interface/tui/subagent"
	roster "tofu/internal/subagent"
)

const (
	askedLine   = "ts-dev-2 asked: may I start the dev server?"
	answerLine  = "orchestrator: no, bash-2 serves it on 3003"
	assumedLine = "orchestrator did not answer, assumed: use port 3004"
)

func askWork() []tui.Event {
	asking := []subagent.Row{{Name: "ts-dev-2", Doing: "build the hono routes", Owns: []string{"src/routes/**"}, Since: time.Second, Steps: 2, Total: 40, State: roster.Working}}
	return append([]tui.Event{
		{Kind: tui.EventToolCall, ID: "5e06aa", Tool: "spawn", Text: "build the hono routes", Promote: true},
		{Kind: tui.EventSubAgent, SubAgents: asking},
		{Kind: tui.EventToolCall, ID: "a5c001", Tool: "ask", Text: "may I start the dev server?", Agent: "ts-dev-2"},
		{Kind: tui.EventToolResult, ID: "a5c001", Text: answerLine, Agent: "ts-dev-2"},
		{Kind: tui.EventToolCall, ID: "a5c002", Tool: "ask", Text: "may I bind port 3004?", Agent: "ts-dev-2"},
		{Kind: tui.EventToolResult, ID: "a5c002", Text: assumedLine, Agent: "ts-dev-2"},
		result("5e06aa", "spawned [&ts-dev-2]"),
	}, done()...)
}

func TestAnAskReadsAsMessagesInBothFeedsAndNotInTheChat(t *testing.T) {
	r, out := launch(t, "ask"+scriptSuffix)
	playAll(t, r, "type ask\nkey enter\nwait cooked for\n", true, out)
	if chat := r.driver.Plain(); strings.Contains(chat, "dev server") || strings.Contains(chat, "3003") {
		t.Errorf("the chat shows the ask\n%s", chat)
	}
	playAll(t, r, "key alt+2\nwait All activity\n", true, out)
	for _, who := range []string{"[&orchestrator]", "[&ts-dev-2]"} {
		playAll(t, r, "key down\n", true, out)
		screen := r.driver.Plain()
		heading, _, _ := strings.Cut(strings.Split(screen, "\n")[3], "·")
		if !strings.Contains(heading, who) {
			t.Fatalf("the feed heading is %q, not %s\n%s", heading, who, screen)
		}
		for _, line := range []string{askedLine, answerLine, assumedLine} {
			if !strings.Contains(screen, line) {
				t.Errorf("%s's feed lacks %q\n%s", who, line, screen)
			}
		}
	}
}
