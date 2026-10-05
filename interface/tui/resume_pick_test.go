package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSlashResumeWithAHandleResumesItAndNeverReachesTheModel(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	var asked []string
	app.options.Resume = func(handle string) (string, []Event) {
		asked = append(asked, handle)
		if handle != "parser" {
			return "no session " + handle + " here", nil
		}
		return "continuing s-bravo", []Event{{Kind: EventSession, ID: "s-bravo", Text: "parser"}, {Kind: EventText, Text: "bravo answered"}}
	}

	typeText(app, "/resume   parser  ")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	typeText(app, "/resume nowhere")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	chat := ansi.Strip(app.View().Content)
	if strings.Join(asked, ",") != "parser,nowhere" || app.top() != nil {
		t.Fatalf("the handles asked were %q with %T open, want parser then nowhere and no picker", asked, app.top())
	}
	for _, want := range []string{"bravo answered", "continuing s-bravo", "no session nowhere here"} {
		if !strings.Contains(chat, want) {
			t.Errorf("the chat does not say %q\n%s", want, chat)
		}
	}
	nothingEntered(t, entered)

	typeText(app, "/resume ")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, picker := app.top().(*recordedDialog); !picker || len(asked) != 2 {
		t.Errorf("/resume and a space opened %T and asked %q, want the picker", app.top(), asked)
	}
}

func TestSlashResumePicksASessionByItsFirstLineAndTheChatShowsItsTurn(t *testing.T) {
	entered := make(chan string, 1)
	app := commandApp(t, entered)
	var resumed []string
	app.options.Sessions = func() ([]SessionRow, error) {
		return []SessionRow{
			{ID: "s-charlie", Task: "tidy the charlie notes", Facts: "1m ago · 1 turn", InUse: true},
			{ID: "s-bravo", Name: "parser", Task: "sketch the bravo parser", Facts: "2m ago · 1 turn"},
			{ID: "s-alpha", Task: "plan the alpha migration", Facts: "3m ago · 1 turn"},
		}, nil
	}
	app.options.Resume = func(id string) (string, []Event) {
		resumed = append(resumed, id)
		return "continuing " + id, []Event{
			{Kind: EventSession, ID: id, Text: "parser"},
			{Kind: EventTask, Text: "sketch the bravo parser"},
			{Kind: EventText, Text: "bravo answered"},
		}
	}

	typeText(app, "/resume")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	listed := ansi.Strip(app.View().Content)
	for _, want := range []string{"tidy the charlie notes", "parser", "plan the alpha migration"} {
		if !strings.Contains(listed, want) {
			t.Errorf("/resume does not list %q\n%s", want, listed)
		}
	}
	for _, line := range strings.Split(listed, "\n") {
		if strings.Contains(line, "●") != strings.Contains(line, "charlie") {
			t.Errorf("the mark and the session in use part on %q\n%s", line, listed)
		}
	}

	typeText(app, "turn")
	if counted := ansi.Strip(app.View().Content); !strings.Contains(counted, "No results") {
		t.Errorf("the filter matched the turn count, want name and first line only\n%s", counted)
	}
	for range "turn" {
		app.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}

	typeText(app, "bravo")
	filtered := ansi.Strip(app.View().Content)
	if !strings.Contains(filtered, "parser") || strings.Contains(filtered, "alpha") || strings.Contains(filtered, "charlie") {
		t.Fatalf("typing bravo does not leave the bravo session alone\n%s", filtered)
	}

	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(resumed) != 1 || resumed[0] != "s-bravo" {
		t.Fatalf("enter resumed %v, want the bravo session by its id", resumed)
	}
	chat := ansi.Strip(app.View().Content)
	if app.top() != nil || !strings.Contains(chat, "bravo answered") || !strings.Contains(chat, "continuing s-bravo") {
		t.Errorf("after enter %T is open and the chat does not show the resumed turn\n%s", app.top(), chat)
	}
	if app.sessionID != "s-bravo" {
		t.Errorf("the app names session %q after the resume, want s-bravo", app.sessionID)
	}
	nothingEntered(t, entered)

	typeText(app, "write a note")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	<-entered
	typeText(app, "/resume parser")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(resumed) != 1 || !strings.Contains(ansi.Strip(app.View().Content), turnRunningNote) {
		t.Errorf("/resume parser during a turn resumed %v and does not say to stop the turn first", resumed)
	}
	typeText(app, "/resume")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if busy := ansi.Strip(app.View().Content); app.top() != nil || !strings.Contains(busy, turnRunningNote) {
		t.Errorf("/resume during a turn opened %T and does not say to stop the turn first\n%s", app.top(), busy)
	}
}
