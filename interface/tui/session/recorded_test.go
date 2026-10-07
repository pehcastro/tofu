package session

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/subagent"
	isession "tofu/internal/session"
	roster "tofu/internal/subagent"
)

const (
	recordedLead   = "0f6e2c1a-aaaa-4bbb-8ccc-00000047b7e1"
	recordedPerson = "0f6e2c1a-aaaa-4bbb-8ccc-0000009c2d40"
	recordedReport = "0f6e2c1a-aaaa-4bbb-8ccc-000000a51c33"
)

func TestEveryMessageDrawsTheIDItWasRecordedUnder(t *testing.T) {
	sent := time.Date(2026, 9, 19, 13, 5, 0, 0, time.UTC)
	model := New(fixed(), counted(new(int)))
	model.SetSize(100, 30)
	model.Append(Entry{Kind: User, Body: "say hi"})
	model.Append(Entry{Kind: User, Body: "say hi"})
	model.Append(Entry{Kind: Assistant, Body: "hi"})
	model.Reported("minted-report", subagent.Row{Name: "ts-dev-1", State: roster.Finished, Report: "wrote the file"})
	before := ansi.Strip(model.View())
	if strings.Contains(before, "[message#") {
		t.Fatalf("a message draws an id before it is recorded, and nothing can quote it:\n%s", before)
	}

	model.Recorded(isession.RoleUser, "<env>cwd</env>\n\nsay hi", recordedPerson, sent)
	model.Recorded(isession.RoleAssistant, "hi\n", recordedLead, sent)
	model.Recorded(isession.RoleUser, "sub-agent ts-dev-1 reports: wrote the file", recordedReport, sent)
	drawn := ansi.Strip(model.View())
	for _, want := range []string{"13:05  [message#9c2d40]", "13:05  [message#47b7e1]", "[message#a51c33]"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the chat does not draw %q:\n%s", want, drawn)
		}
	}
	if strings.Count(drawn, "[message#9c2d40]") != 1 {
		t.Errorf("one recorded message named two chat entries:\n%s", drawn)
	}
	if strings.Count(drawn, "14:32") != 1 {
		t.Errorf("the second say hi, not yet recorded, should keep its own clock and no other entry should:\n%s", drawn)
	}
}

func TestAReplyAfterANewRequestStartsItsOwnMessage(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(80, 24)
	model.Start()
	model.Stream("want me to run it?")
	model.Requesting()
	model.Stream("ran it.")
	if got := strings.Count(ansi.Strip(model.View()), "[&orchestrator]"); got != 2 {
		t.Fatalf("the continuation reply is glued to the reply before it, %d orchestrator rows:\n%s", got, ansi.Strip(model.View()))
	}
}

func TestOnlyTheAwaitedAskIsDrawnAndAnsweredByItsID(t *testing.T) {
	model := New(fixed(), counted(new(int)))
	model.SetSize(120, 30)
	model.Start()
	model.Append(Entry{Kind: Tool, ID: "c-old", Head: "message", Body: "ts-dev-5: reply with the single word back"})
	model.Decide("ask-old", Decision{Tool: "message", Verdict: Ask})
	model.Finish("c-old", Result{Status: "refused"})
	model.Await("ask-hooks", "hooks", "hooks GateVerdict *:", nil)
	drawn := ansi.Strip(model.View())
	if strings.Contains(drawn, "? message wants") {
		t.Fatalf("an ask whose call already ended is drawn under the trust prompt:\n%s", drawn)
	}
	if !strings.Contains(drawn, "? hooks wants") || model.AskedID() != "ask-hooks" {
		t.Fatalf("the trust ask is not the one drawn, asked id %q:\n%s", model.AskedID(), drawn)
	}

	model.Append(Entry{Kind: Tool, ID: "c-push", Head: "bash", Body: "git push --force"})
	model.Decide("ask-push", Decision{Tool: "bash", Verdict: Ask})
	model.Await("ask-push", "bash", "bash git push", nil)
	if model.AskedID() != "ask-hooks" {
		t.Fatalf("a second ask took the keys from the open one, asked id %q", model.AskedID())
	}
	model.Resume("ask-hooks")
	drawn = ansi.Strip(model.View())
	if model.AskedID() != "ask-push" || !strings.Contains(drawn, "? bash wants git push --force") {
		t.Fatalf("the ask still waiting is not drawn once the first is answered, asked id %q:\n%s", model.AskedID(), drawn)
	}
	model.Resume("ask-push")
	if model.Awaiting() || model.AskedID() != "" {
		t.Fatalf("an answered ask still holds the keys, asked id %q", model.AskedID())
	}
}
