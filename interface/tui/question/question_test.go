package question

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/internal/golden"
	"tofu/internal/turn"
)

func zero() *int { return new(int) }

func one() *int {
	at := 1
	return &at
}

func asked() []turn.PersonQuestion {
	return []turn.PersonQuestion{
		{ID: "lib", Header: "HTTP client", Question: "Which HTTP client should the fetcher use?", Type: turn.QuestionChoice, Recommended: zero(),
			Options: []turn.PersonOption{{Label: "net/http", Description: "standard library, no dependency"}, {Label: "resty", Description: "retries built in, one dependency", Preview: "client := resty.New().SetRetryCount(3)"}}},
		{ID: "extras", Header: "Extras", Question: "Which extras should it carry?", Type: turn.QuestionMulti, Recommended: zero(),
			Options: []turn.PersonOption{{Label: "retries", Description: "three tries"}, {Label: "tracing", Description: "spans per request"}, {Label: "caching", Description: "etag cache"}}},
		{ID: "push", Header: "Push", Question: "Push the branch when done?", Type: turn.QuestionYesNo, Recommended: one(),
			Options: []turn.PersonOption{{Label: "yes"}, {Label: "no"}}},
		{ID: "name", Header: "Name", Question: "What is the module called?", Type: turn.QuestionText},
	}
}

func openedAt() time.Time { return time.Date(2026, 10, 8, 20, 0, 0, 0, time.UTC) }

func plain(lines []string) string { return ansi.Strip(strings.Join(lines, "\n")) }

func TestAFormAnswersEveryKindOfQuestion(t *testing.T) {
	form := Open("q1", asked(), 0, openedAt())
	steps := []struct{ key, composer string }{
		{"down", ""}, {"enter", ""},
		{"space", ""}, {"down", ""}, {"down", ""}, {"space", ""}, {"enter", ""},
		{"1", ""},
		{"enter", "fetchx"},
	}
	action := Kept
	for i, step := range steps {
		var took bool
		action, took = form.Key(step.key, step.composer)
		if !took {
			t.Fatalf("step %d %q was not taken", i, step.key)
		}
	}
	if action != Submitted {
		t.Fatalf("the last step left the form %v, want submitted", action)
	}
	got := form.Replies()
	want := []string{"lib resty |", "extras retries,caching |", "push yes |", "name  |fetchx"}
	for i, reply := range got {
		if line := reply.ID + " " + strings.Join(reply.Chosen, ",") + " |" + reply.Text; line != want[i] {
			t.Errorf("reply %d reads %q, want %q", i, line, want[i])
		}
	}
}

func TestAFormLeavesTypingToTheComposer(t *testing.T) {
	form := Open("q1", asked(), 0, openedAt())
	for _, key := range []string{"a", "9", "enter"} {
		composer := ""
		if key == "enter" {
			composer = "hello"
		}
		if _, took := form.Key(key, composer); took {
			t.Errorf("%q with %q in the composer was taken by the form, and it belongs to the chat", key, composer)
		}
	}
	if action, took := form.Key("esc", ""); !took || action != Dismissed {
		t.Fatalf("esc gave %v %v, want dismissed", action, took)
	}
}

func TestAFormMarksTheRecommendedAndSaysWhenItAutoSelects(t *testing.T) {
	opened := openedAt()
	form := Open("q1", asked(), 2*time.Minute, opened)
	before := plain(form.Lines(80, opened.Add(8*time.Second)))
	for _, want := range []string{"HTTP client", "1 of 4", "net/http", "(recommended)", "standard library, no dependency", "other", "auto-selects net/http in"} {
		if !strings.Contains(before, want) {
			t.Errorf("the form does not show %q:\n%s", want, before)
		}
	}
	if strings.Contains(before, "resty.New") {
		t.Errorf("an unfocused option's preview is drawn:\n%s", before)
	}
	form.Key("down", "")
	focused := plain(form.Lines(80, opened.Add(9*time.Second)))
	if !strings.Contains(focused, "resty.New") {
		t.Errorf("the focused option's preview is not drawn:\n%s", focused)
	}
	after := plain(form.Lines(80, opened.Add(3*time.Minute)))
	if !strings.Contains(after, "auto-selected net/http after timeout") {
		t.Errorf("past the wait the form does not say it auto-selected:\n%s", after)
	}
	for _, line := range form.Lines(80, opened) {
		if cells := ansi.StringWidth(line); cells != 80 {
			t.Errorf("a line is %d cells wide at 80 columns: %q", cells, ansi.Strip(line))
		}
	}
	golden.Assert(t, "question-form-80.golden", plain(form.Lines(80, opened.Add(3*time.Minute))))
}
