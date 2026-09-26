package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"tofu/interface/tui/models"
	"tofu/internal/llm"
	library "tofu/internal/llm/models"
)

const (
	pressesPastEveryRow = 64
	shippedRoot         = "../../library"
	sonnet              = "claude-sub/claude-sonnet-5"
)

func shippedFixture() (library.Library, error) {
	return library.Load([]library.Layer{{Name: "library", Origin: "library", FS: os.DirFS(shippedRoot)}})
}

func openPicker(t *testing.T, app *App) *models.Model {
	t.Helper()
	dialog, open := app.top().(*modelsDialog)
	if !open {
		t.Fatalf("the models dialog is not open, the top dialog is %T", app.top())
	}
	return &dialog.picker
}

func walkTo(t *testing.T, app *App, slug string) {
	t.Helper()
	for range pressesPastEveryRow {
		if row, picked := openPicker(t, app).Picked(); picked && row.Slug == slug {
			return
		}
		app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	t.Fatalf("the picker never landed on %s", slug)
}

func pickSonnet(t *testing.T, app *App) {
	t.Helper()
	walkTo(t, app, sonnet)
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

func pickerApp(t *testing.T, ran chan Pick, wires func() []Wire) *App {
	t.Helper()
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  wires,
		Models: shippedFixture,
		Turn: func(_ context.Context, pick Pick, _ string, _ CalledFromInsideTheTurnAndNeverAfterItReturns) {
			ran <- pick
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	app.openPicker(false)
	return app
}

func turnPick(t *testing.T, app *App, ran chan Pick) Pick {
	t.Helper()
	typeText(app, "read one file")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case pick := <-ran:
		return pick
	case <-time.After(2 * time.Second):
		t.Fatal("the turn never started")
	}
	return Pick{}
}

func TestPickingAModelInsideASubscriptionRunsThatModel(t *testing.T) {
	ran := make(chan Pick, 1)
	app := pickerApp(t, ran, bothWires)
	pickSonnet(t, app)
	pick := turnPick(t, app, ran)
	if pick.Model != sonnet {
		t.Errorf("the turn ran on model %q, want the picked %s", pick.Model, sonnet)
	}
	if pick.Wire != "anthropic" {
		t.Errorf("the turn ran on wire %q, want anthropic, which serves %s", pick.Wire, sonnet)
	}
	if pick.Effort != llm.EffortDefault {
		t.Errorf("the turn ran at effort %q, want the %s the picker showed", pick.Effort, llm.EffortDefault)
	}
}

func TestThePickerCarriesTheEffortChosenBesideTheModel(t *testing.T) {
	ran := make(chan Pick, 1)
	app := pickerApp(t, ran, bothWires)
	walkTo(t, app, sonnet)
	app.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift})
	shown := ansi.Strip(app.View().Content)
	if !strings.Contains(shown, "high  shift+←→") {
		t.Fatalf("the picker does not say the effort it would send\n%s", shown)
	}
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if pick := turnPick(t, app, ran); pick.Effort != llm.EffortHigh {
		t.Errorf("the turn ran at effort %q, want the picked high", pick.Effort)
	}
}

func TestTheEffortOffTheEndOfTheListStaysWhereItIs(t *testing.T) {
	ran := make(chan Pick, 1)
	app := pickerApp(t, ran, anthropicAlone)
	for range pressesPastEveryRow {
		app.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
	}
	if lowest := openPicker(t, app).Effort(); lowest != llm.EffortLow {
		t.Fatalf("the effort walked to %q, and the anthropic wire offers %s", lowest, llm.EffortList(claudeEfforts()))
	}
}

func TestThePickerNoLongerSaysTheModelDoesNotReachTheTurn(t *testing.T) {
	ran := make(chan Pick, 1)
	app := pickerApp(t, ran, bothWires)
	pickSonnet(t, app)
	shown := app.View().Content
	for _, stopgap := range []string{"does not reach the turn", "not reach the turn yet"} {
		if strings.Contains(shown, stopgap) {
			t.Errorf("the interface still carries the stopgap sentence %q\n%s", stopgap, shown)
		}
	}
	if !strings.Contains(shown, pickedHead+sonnet) {
		t.Errorf("the note does not say the next turn runs %s\n%s", sonnet, shown)
	}
}

func TestPickingAModelFromAnotherSubscriptionReachesTheTurn(t *testing.T) {
	ran := make(chan Pick, 1)
	app := newTestApp(Options{
		Repo:   testRepo,
		Branch: "develop",
		Now:    fixedClock(),
		Wires:  bothWires,
		Models: shippedFixture,
		Turn: func(_ context.Context, pick Pick, _ string, _ CalledFromInsideTheTurnAndNeverAfterItReturns) {
			ran <- pick
		},
	})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	if app.wire != "codex" {
		t.Fatalf("the app opened on wire %q, want the first signed wire codex", app.wire)
	}
	typeText(app, "/models")
	app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	openPicker(t, app)
	pickSonnet(t, app)
	if app.wire != "anthropic" {
		t.Fatalf("the pick left the app on wire %q, want anthropic", app.wire)
	}
	if pick := turnPick(t, app, ran); pick.Wire != "anthropic" {
		t.Fatalf("the turn ran on %q, want the picked anthropic", pick.Wire)
	}
}

func TestThePickerOffersOnlyTheSubscriptionsTheTurnCanRun(t *testing.T) {
	loaded, err := shippedFixture()
	if err != nil {
		t.Fatal(err)
	}
	if _, carried := loaded.ForWire("codex"); !carried {
		t.Fatal("the library carries no codex subscription, so a wire nobody signed in to cannot be shown missing")
	}
	app := newTestApp(Options{Repo: testRepo, Now: fixedClock(), Wires: anthropicAlone, Models: shippedFixture})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	app.openPicker(false)
	signed := map[string]bool{}
	for _, wire := range app.wires {
		signed[wire.Provider] = true
	}
	groups := openPicker(t, app).Groups
	if len(groups) != len(signed) {
		t.Fatalf("the picker shows %d subscriptions and the turn can run %d", len(groups), len(signed))
	}
	for _, group := range groups {
		if !signed[group.Source] {
			t.Errorf("the picker offers %s, which no signed wire runs", group.Source)
		}
	}
	if shown := app.View().Content; strings.Contains(shown, "codex-sub/") {
		t.Errorf("the picker names a model of a subscription nobody signed in to\n%s", shown)
	}
}

func TestAModelOnASubscriptionTheTurnCannotRunIsNotReached(t *testing.T) {
	app := newTestApp(Options{Repo: testRepo, Now: fixedClock(), Wires: anthropicAlone, Models: shippedFixture})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	app.runNextTurnOn("codex-sub/gpt-5.6-sol", llm.EffortHigh)
	if app.picked != "" || app.wire != "anthropic" {
		t.Fatalf("a slug reached wire %q with model %q, and no signed wire serves codex-sub", app.wire, app.picked)
	}
}

func TestAnExcludedModelIsNeverWhatTheNextTurnRunsOn(t *testing.T) {
	loaded, err := shippedFixture()
	if err != nil {
		t.Fatal(err)
	}
	excluded := map[string]bool{}
	for _, one := range loaded.Models {
		if one.Use == library.UseExcluded {
			excluded[one.Slug()] = true
		}
	}
	if len(excluded) == 0 {
		t.Skip("the shipped library excludes no model, so no pick here can land on one")
	}
	ran := make(chan Pick, 1)
	for press := range pressesPastEveryRow {
		app := pickerApp(t, ran, bothWires)
		for range press {
			app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		}
		app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if excluded[app.picked] {
			t.Fatalf("%d presses and enter left the next turn on %s, which the library excludes", press, app.picked)
		}
	}
}

func TestThePickSurvivesThePickerClosing(t *testing.T) {
	app := newTestApp(Options{Repo: testRepo, Now: fixedClock(), Wires: bothWires, Models: shippedFixture})
	app.Init()
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	app.openPicker(false)
	pickSonnet(t, app)
	if app.current != screenChat || app.top() != nil {
		t.Fatalf("enter left the app on screen %d with %T open, want chat", app.current, app.top())
	}
	app.Update(tea.KeyPressMsg{Code: '4', Mod: tea.ModAlt})
	app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.wire != "anthropic" {
		t.Fatalf("the pick did not survive the screen closing: the app is on wire %q", app.wire)
	}
	if !strings.Contains(app.View().Content, sonnet) {
		t.Errorf("the header does not name the subscription and model the next turn runs\n%s", app.View().Content)
	}
}

func TestTheShippedLibraryIsWhatThePickerReadsWhenNobodyPassesOne(t *testing.T) {
	loaded, err := New(Options{Repo: testRepo, Now: fixedClock()}).options.Models()
	if err != nil {
		t.Skipf("the model library on this machine does not load, so the default cannot be read here: %v", err)
	}
	if len(loaded.Models) == 0 {
		t.Fatal("the default model source carries no model, so /models would open on nothing")
	}
}
