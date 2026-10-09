package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/llm"
	"tofu/internal/subagent"
)

type failingGate struct{}

func (failingGate) Decide(context.Context, GateRequest) (GateDecision, error) {
	return GateDecision{Verdict: ledger.VerdictAsk}, errors.New("jev answered 503")
}

type unavailableGate struct{}

func (unavailableGate) Decide(context.Context, GateRequest) (GateDecision, error) {
	return GateDecision{ID: "row-fallback", Verdict: ledger.VerdictAsk, Failure: "jev answered 503"}, nil
}

type writeRun struct {
	ctx    context.Context
	gate   Gate
	mode   GateMode
	person Person
	path   string
}

func (w writeRun) do(t *testing.T) (ToolCallRow, string, bool) {
	t.Helper()
	root := t.TempDir()
	write, err := NewWriteTool(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := w.ctx
	if ctx == nil {
		ctx = t.Context()
	}
	model := &leadAndSubAgent{lead: []llm.Decision{called("write", map[string]any{"path": w.path, "content": "a note"}), claimDecision("done")}}
	row, err := Run(ctx, Config{Model: model, Spend: SpendAPIKey, Tools: NewRegistry(write), Caps: Caps{MaxSteps: 4}, ResultBytesCap: 4096, Project: root,
		ArtifactDir: filepath.Join(root, "artifacts"), Gate: w.gate, GateMode: w.mode, Person: w.person, Task: "write a note"})
	if err != nil {
		t.Fatal(err)
	}
	said := ""
	if len(model.leadSaw) > 1 {
		said = model.leadSaw[1]
	}
	_, statErr := os.Stat(filepath.Join(root, w.path))
	return row.Steps[0].ToolCalls[0], said, statErr == nil
}

func countingPerson(asked *atomic.Int32, answer PersonAnswer) Person {
	return func(context.Context, GateRequest, GateDecision) (PersonAnswer, error) {
		asked.Add(1)
		return answer, nil
	}
}

func sessionAsking(asks bool) context.Context {
	return WithQuestionsBlock(context.Background(), func() (bool, bool) { return asks, true })
}

func TestAGateErrorAsksInAskModeAndRefusesUnaskedInAuto(t *testing.T) {
	for _, gate := range []Gate{failingGate{}, unavailableGate{}} {
		for _, c := range []struct {
			name      string
			auto      bool
			answer    PersonAnswer
			wantAsked int32
			wantRan   bool
			wantSaid  string
		}{
			{"ask mode, allowed", false, PersonAllowedOnce, 1, true, ""},
			{"ask mode, denied", false, PersonDenied, 1, false, "the person did not allow it"},
			{"auto mode", true, PersonAllowedOnce, 0, false, "jev answered 503"},
		} {
			t.Run(c.name, func(t *testing.T) {
				var asked atomic.Int32
				person := countingPerson(&asked, c.answer)
				if c.auto {
					person = person.RunsWhatJevAsks()
				}
				call, said, wrote := writeRun{gate: gate, mode: GateEnforce, person: person, path: "note.txt"}.do(t)
				if asked.Load() != c.wantAsked || wrote != c.wantRan || call.Refused == c.wantRan {
					t.Fatalf("%T: asked %d, wrote %v, refused %v; want asked %d, ran %v; the model read %q", gate, asked.Load(), wrote, call.Refused, c.wantAsked, c.wantRan, said)
				}
				if !strings.Contains(said, c.wantSaid) {
					t.Errorf("%T: the model read %q, want it to carry %q", gate, said, c.wantSaid)
				}
			})
		}
		call, said, wrote := writeRun{gate: gate, mode: GateEnforce, path: "note.txt"}.do(t)
		if wrote || !call.Refused || !strings.Contains(said, "no person was available") {
			t.Errorf("%T: with no person a failed gate wrote %v, refused %v, said %q", gate, wrote, call.Refused, said)
		}
	}
}

func TestAPersonOnlyCallIsAskedBeforeTheGateWhenTheGateWillNotAsk(t *testing.T) {
	harness := filepath.Join(".tofu", "settings.json")
	for _, c := range []struct {
		name      string
		gate      Gate
		mode      GateMode
		answer    PersonAnswer
		noPerson  bool
		path      string
		wantAsked int32
		wantRan   bool
	}{
		{"no gate, no person", nil, GateShadow, PersonAllowedOnce, true, harness, 0, false},
		{"no gate, the person allows", nil, GateShadow, PersonAllowedOnce, false, harness, 1, true},
		{"no gate, the person refuses", nil, GateShadow, PersonDenied, false, harness, 1, false},
		{"shadow gate, the person refuses", asksWithReason{}, GateShadow, PersonDenied, false, harness, 1, false},
		{"enforced gate asks itself, once", asksWithReason{}, GateEnforce, PersonAllowedOnce, false, harness, 1, true},
		{"no gate, an ordinary file", nil, GateShadow, PersonDenied, false, "note.txt", 0, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			var asked atomic.Int32
			person := countingPerson(&asked, c.answer)
			if c.noPerson {
				person = nil
			}
			call, said, wrote := writeRun{gate: c.gate, mode: c.mode, person: person, path: c.path}.do(t)
			if asked.Load() != c.wantAsked || wrote != c.wantRan || call.Refused == c.wantRan {
				t.Fatalf("asked %d, wrote %v, refused %v; want asked %d, ran %v; the model read %q", asked.Load(), wrote, call.Refused, c.wantAsked, c.wantRan, said)
			}
			if !c.wantRan && !strings.Contains(said, "only the person") {
				t.Errorf("the refusal does not say only the person allows it: %q", said)
			}
		})
	}
}

func TestTheSessionsAskingModeDecidesOverTheSettingAndTheSettingDecidesWithoutIt(t *testing.T) {
	for _, c := range []struct {
		name       string
		ctx        context.Context
		settingAsk bool
		wantAsked  int32
	}{
		{"session ask over setting auto", sessionAsking(true), false, 1},
		{"session auto over setting ask", sessionAsking(false), true, 0},
		{"no session mode, setting ask", nil, true, 1},
		{"no session mode, setting auto", nil, false, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			var asked atomic.Int32
			person := countingPerson(&asked, PersonAllowedOnce).Asking(func() bool { return c.settingAsk })
			call, _, wrote := writeRun{ctx: c.ctx, gate: asksWithReason{reason: true}, mode: GateEnforce, person: person, path: "note.txt"}.do(t)
			if asked.Load() != c.wantAsked || call.Refused || !wrote {
				t.Errorf("asked %d times, refused %v, wrote %v; want asked %d, run", asked.Load(), call.Refused, wrote, c.wantAsked)
			}
		})
	}
}

func TestALeadStopWithdrawsThePersonsWaitAndTheCallIsRefused(t *testing.T) {
	inbox := NewInbox()
	entered := make(chan struct{})
	waits := Person(func(ctx context.Context, _ GateRequest, _ GateDecision) (PersonAnswer, error) {
		close(entered)
		<-ctx.Done()
		return PersonDenied, ctx.Err()
	})
	go func() {
		<-entered
		inbox.StopLead()
	}()
	done := make(chan struct{})
	var call ToolCallRow
	var said string
	go func() {
		defer close(done)
		call, said, _ = writeRun{gate: asksWithReason{reason: true}, mode: GateEnforce, person: inbox.LeadAsks(waits), path: "note.txt"}.do(t)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a lead stop left the person's wait open")
	}
	if !call.Refused || !strings.Contains(said, leadStopped{}.Error()) {
		t.Errorf("refused %v, the model read %q", call.Refused, said)
	}
}

func TestASubAgentsWaitForTheLeadPausesWhileTheLeadWaitsOnThePerson(t *testing.T) {
	const wait = 150 * time.Millisecond
	inbox := NewInbox()
	held := &heldSubAgent{agent: subagent.SubAgent{ID: "sub-1"}}
	inbox.held["sub-1"] = held
	release := make(chan struct{})
	personHolds := inbox.LeadAsks(func(context.Context, GateRequest, GateDecision) (PersonAnswer, error) {
		<-release
		return PersonAllowedOnce, nil
	})
	go func() { _, _ = personHolds(context.Background(), GateRequest{}, GateDecision{}) }()
	time.Sleep(20 * time.Millisecond)
	answer := inbox.ask(held, "may sub-1 write")
	type outcome struct {
		allowed bool
		err     error
	}
	got := make(chan outcome, 1)
	go func() {
		allowed, err := inbox.awaitLead(context.Background(), held, answer, "may sub-1 write", wait)
		got <- outcome{allowed, err}
	}()
	select {
	case early := <-got:
		t.Fatalf("the sub-agent stopped waiting while the lead waited on the person: %+v", early)
	case <-time.After(3 * wait):
	}
	close(release)
	if _, answered := inbox.answer("sub-1", true); !answered {
		t.Fatal("the sub-agent's ask was withdrawn before the lead could answer it")
	}
	if out := <-got; !out.allowed || out.err != nil {
		t.Errorf("the lead's allow reads %+v", out)
	}

	unanswered := inbox.ask(held, "may sub-1 write again")
	started := time.Now()
	allowed, err := inbox.awaitLead(context.Background(), held, unanswered, "may sub-1 write again", wait)
	if took := time.Since(started); allowed || err == nil || took < wait || took > 3*wait {
		t.Errorf("with the lead free the wait took %s and read %v, %v; want a refusal after %s", took, allowed, err, wait)
	}
}
