package question

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"tofu/internal/agent/loop"
	"tofu/internal/testkit/golden"
)

func zero() *int { return new(int) }

func one() *int {
	at := 1
	return &at
}

func asked() []loop.PersonQuestion {
	return []loop.PersonQuestion{
		{ID: "lib", Header: "HTTP client", Question: "Which HTTP client should the fetcher use?", Type: loop.QuestionChoice, Recommended: zero(),
			Options: []loop.PersonOption{{Label: "net/http", Description: "standard library, no dependency"}, {Label: "resty", Description: "retries built in, one dependency", Preview: "client := resty.New().SetRetryCount(3)"}}},
		{ID: "extras", Header: "Extras", Question: "Which extras should it carry?", Type: loop.QuestionMulti, Recommended: zero(),
			Options: []loop.PersonOption{{Label: "retries", Description: "three tries"}, {Label: "tracing", Description: "spans per request"}, {Label: "caching", Description: "etag cache"}}},
		{ID: "push", Header: "Push", Question: "Push the branch when done?", Type: loop.QuestionYesNo, Recommended: one(),
			Options: []loop.PersonOption{{Label: "yes"}, {Label: "no"}}},
		{ID: "name", Header: "Name", Question: "What is the module called?", Type: loop.QuestionText},
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
	form := Open("q1", asked()[1:], 0, openedAt())
	for _, key := range []string{"a", "9", "enter"} {
		composer := ""
		if key == "enter" {
			composer = "hello"
		}
		if _, took := form.Key(key, composer); took {
			t.Errorf("%q with %q in the composer was taken by the form, and it belongs to the chat", key, composer)
		}
	}
	before := plain(form.Lines(80, 40, 0, openedAt()))
	if action, took := form.Key("esc", ""); !took || action != Kept {
		t.Fatalf("esc gave %v %v, want kept and taken so nothing else reads it", action, took)
	}
	if after := plain(form.Lines(80, 40, 0, openedAt())); after != before {
		t.Fatalf("esc changed the form:\n%s\nwant\n%s", after, before)
	}
	for range 4 {
		form.Key("down", "")
	}
	if action, took := form.Key("enter", ""); !took || action != Dismissed {
		t.Fatalf("enter on return gave %v %v, want dismissed", action, took)
	}
}

func TestALongAskWrapsAndNeverCuts(t *testing.T) {
	long := strings.Repeat("every word of this description must reach the screen ", 4) + "END"
	asked := []loop.PersonQuestion{{ID: "q", Header: "Retry policy", Question: strings.Repeat("a long question ", 12) + "TAIL", Type: loop.QuestionChoice, Recommended: zero(),
		Options: []loop.PersonOption{{Label: "first", Description: long}, {Label: "second", Description: "short"}}}}
	form := Open("q1", asked, 0, openedAt())
	text := plain(form.Lines(60, 60, 2, openedAt()))
	for _, want := range []string{"the full question is in the chat above", "1. first (recommended)", "2 more asks wait", "return"} {
		if !strings.Contains(text, want) {
			t.Errorf("the form does not show %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"TAIL", "END", "short"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("the form repeats %q, which the chat row already holds:\n%s", unwanted, text)
		}
	}
	if strings.Contains(text, "…") {
		t.Errorf("the form cut a line:\n%s", text)
	}
	if short := form.Lines(60, 12, 0, openedAt()); len(short) > 12 {
		t.Errorf("a form in 12 rows drew %d", len(short))
	}
}

func TestAFormMarksTheRecommendedAndNeverAutoSelects(t *testing.T) {
	opened := openedAt()
	form := Open("q1", asked(), 2*time.Minute, opened)
	before := plain(form.Lines(80, 60, 0, opened.Add(8*time.Second)))
	for _, want := range []string{"HTTP client", "1 of 4", "net/http", "(recommended)", "other", "answer whenever", "the lead keeps working", "in 1m 52s"} {
		if !strings.Contains(before, want) {
			t.Errorf("the form does not show %q:\n%s", want, before)
		}
	}
	if strings.Contains(before, "standard library, no dependency") || strings.Contains(before, "Which HTTP client") {
		t.Errorf("the form repeats the question or a description, which the chat row already holds:\n%s", before)
	}
	if narrow := plain(form.Lines(40, 60, 0, opened)); strings.Contains(narrow, "resty.New") {
		t.Errorf("too narrow for columns, an unfocused option's preview is drawn:\n%s", narrow)
	}
	form.Key("down", "")
	if focused := plain(form.Lines(40, 60, 0, opened.Add(9*time.Second))); !strings.Contains(focused, "resty.New") {
		t.Errorf("too narrow for columns, the focused option's preview is not drawn:\n%s", focused)
	}
	after := plain(form.Lines(80, 60, 0, opened.Add(3*time.Minute)))
	if !strings.Contains(after, "answer whenever") || !strings.Contains(after, "still hears your answer") || strings.Contains(after, "select") {
		t.Errorf("past the wait the form does not say no answer came and the question is still open:\n%s", after)
	}
	if text := plain(Open("q2", asked()[3:], 2*time.Minute, opened).Lines(80, 60, 0, opened.Add(3*time.Minute))); !strings.Contains(text, "answer whenever") {
		t.Errorf("a text question with no recommended option does not say no answer came:\n%s", text)
	}
	for _, line := range form.Lines(80, 60, 0, opened) {
		if cells := ansi.StringWidth(line); cells != 80 {
			t.Errorf("a line is %d cells wide at 80 columns: %q", cells, ansi.Strip(line))
		}
	}
	golden.Assert(t, "question-form-80.golden", plain(form.Lines(80, 60, 0, opened.Add(3*time.Minute))))
}
