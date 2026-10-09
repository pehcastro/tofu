package host

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/session"
	"tofu/internal/turn"
)

func answeredStanding(t *testing.T, h *Host, request turn.GateRequest, given Answer) turn.PersonAnswer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	asked := make(chan string, 1)
	emit := func(event Event) {
		if event.Kind == EventAwaitPerson {
			asked <- event.ID
		}
	}
	got := make(chan turn.PersonAnswer, 1)
	go func() {
		answer, _ := awaitPerson(emit, h.asks)(ctx, request, turn.GateDecision{Verdict: ledger.VerdictAsk})
		got <- answer
	}()
	select {
	case id := <-asked:
		h.AnswerAsk(id, given)
	case answer := <-got:
		return answer
	case <-ctx.Done():
		t.Fatalf("%s was neither asked nor answered", request.Args)
	}
	return <-got
}

func unasked(t *testing.T, h *Host, request turn.GateRequest) turn.PersonAnswer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	answer, _ := awaitPerson(func(event Event) {
		if event.Kind == EventAwaitPerson {
			t.Errorf("%s asked again after a standing answer was given for it", request.Args)
			cancel()
		}
	}, h.asks)(ctx, request, turn.GateDecision{Verdict: ledger.VerdictAsk})
	return answer
}

func TestAStandingAnswerOutlivesTheHostAndNeverLeavesItsSession(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	first, _ := New(Config{Dir: project})
	here, err := first.OpenFresh()
	if err != nil {
		t.Fatal(err)
	}
	answeredStanding(t, first, settingsAsk("3"), AlwaysHere)
	answeredStanding(t, first, settingsAsk("9"), NeverHere)
	answeredStanding(t, first, settingsAsk("5"), AllowedOnce)
	want := first.Standing()
	if len(want) != 2 {
		t.Fatalf("the first host stands on %v, want allow_always and reject_always and nothing for allow_once", want)
	}
	first.Close()

	second, troubles := New(Config{Dir: project, Resumed: Carry{Session: here}})
	defer second.Close()
	if len(troubles) > 0 {
		t.Fatal(troubles)
	}
	if got := second.Standing(); !slices.Equal(got, want) {
		t.Fatalf("after a restart session.state stands on %v, want %v", got, want)
	}
	if answer := unasked(t, second, settingsAsk("3")); answer != turn.PersonAlwaysHere {
		t.Errorf("after a restart the allow_always call answered %v, want it run unasked", answer)
	}
	if answer := unasked(t, second, settingsAsk("9")); answer != turn.PersonDenied {
		t.Errorf("after a restart the reject_always call answered %v, want it refused unasked", answer)
	}

	if _, err := second.OpenFresh(); err != nil {
		t.Fatal(err)
	}
	if got := second.Standing(); len(got) != 0 {
		t.Errorf("a fresh session stands on %v, the answers of another session", got)
	}
	if answer := answeredStanding(t, second, settingsAsk("3"), Denied); answer != turn.PersonDenied {
		t.Errorf("a fresh session answered %v for a call only another session stood on", answer)
	}
	if _, err := second.Resume(Carry{Session: here}); err != nil {
		t.Fatal(err)
	}
	if got := second.Standing(); !slices.Equal(got, want) {
		t.Errorf("back in the first session it stands on %v, want %v", got, want)
	}
}

func TestAStandingFileThatDoesNotReadStandsOnNothing(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"torn":    `[{"target":"settings`,
		"unknown": `[{"target":"` + askedPlace(settingsAsk("3")) + `","decision":"allow_once"},{"target":"` + askedPlace(settingsAsk("3")) + `","decision":"remember_global"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			id := session.NewEventID()
			if err := os.MkdirAll(store.Dir(id), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(store.Dir(id), "standing.json"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			h, _ := New(Config{Dir: project, Resumed: Carry{Session: id}})
			defer h.Close()
			if got := h.Standing(); len(got) != 0 {
				t.Errorf("a %s file stands on %v, want nothing so the call asks", name, got)
			}
			if answer := answeredStanding(t, h, settingsAsk("3"), Denied); answer != turn.PersonDenied {
				t.Errorf("a %s file answered %v without asking", name, answer)
			}
		})
	}
}

func TestAStandingAnswerThatCannotBeWrittenSaysSo(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, _ := New(Config{Dir: project})
	defer h.Close()
	here, err := h.OpenFresh()
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(store.Dir(here), "standing.json", "blocked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if answer := answeredStanding(t, h, settingsAsk("3"), AlwaysHere); answer != turn.PersonAlwaysHere {
		t.Fatalf("an unwritable file answered %v, want the answer to hold for now", answer)
	}
	for {
		select {
		case event := <-h.Events():
			if event.Kind == EventNote && strings.Contains(event.Text, "only until tofu restarts") {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("a standing answer that was not written said nothing")
		}
	}
}

func TestACompactedSessionKeepsTheStandingAnswersOfTheOneItContinues(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, _ := New(Config{Dir: project})
	defer h.Close()
	before, err := h.OpenFresh()
	if err != nil {
		t.Fatal(err)
	}
	answeredStanding(t, h, settingsAsk("3"), AlwaysHere)
	store, err := session.OpenIn(project)
	if err != nil {
		t.Fatal(err)
	}
	into := session.NewEventID()
	if err := store.Write(session.Header{ID: before, ForkedInto: into}, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Compacted(into, nil); err != nil {
		t.Fatal(err)
	}
	if answer := unasked(t, h, settingsAsk("3")); answer != turn.PersonAlwaysHere {
		t.Errorf("after compaction into %s the allow_always call answered %v, want it run unasked", into, answer)
	}
	h.Close()
	reopened, _ := New(Config{Dir: project, Resumed: Carry{Session: into}})
	defer reopened.Close()
	if got := reopened.Standing(); len(got) != 1 {
		t.Errorf("the compacted session reopened stands on %v, want the answer carried from %s", got, before)
	}
}
