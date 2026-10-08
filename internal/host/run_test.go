package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/session"
	roster "tofu/internal/subagent"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

const overrideAsked = "no_unit_test_after_code"

func TestTheGateOffEventCarriesTheReasonTheKeyLookupFound(t *testing.T) {
	t.Setenv("OPENROUTER"+"_KEY", "")
	dir := t.TempDir()
	unnamed := filepath.Join(dir, "unnamed", ".env")
	unreadable := filepath.Join(dir, "unreadable", ".env")
	if err := os.MkdirAll(filepath.Dir(unnamed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unnamed, []byte("OTHER=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(unreadable, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, state := range []struct {
		name string
		path string
		want jev.Why
	}{
		{"no file", filepath.Join(dir, "gone", ".env"), jev.WhyNoFile},
		{"a file without the name in it", unnamed, jev.WhyFileLacksName},
		{"a file that will not read", unreadable, jev.WhyUnreadable},
	} {
		_, err := jev.Key(state.path)
		if err == nil {
			t.Fatalf("%s was accepted as a key", state.name)
		}
		event := gateOffEvent(err)
		if event.Kind != EventGateOff || event.Text != err.Error() {
			t.Errorf("%s produced %+v, want the gate-off event carrying %q", state.name, event, err.Error())
		}
		if event.GateWhy != state.want {
			t.Errorf("%s reads as reason %d, want %d: %v", state.name, event.GateWhy, state.want, err)
		}
	}
}

func recordedGateDecision() turn.GateDecision {
	modeReason := "the rule came from the library as tool_gate@3.yaml"
	return turn.GateDecision{
		ID:      "2026-09-19-6f1c",
		Verdict: ledger.VerdictAsk,
		Answers: []ledger.Answer{
			{Question: "approval", Kind: ledger.AnswerNoul, Noul: 0.75},
			{Question: "from_untrusted", Kind: ledger.AnswerNoul, Noul: 0.02},
			{Question: "risk", Kind: ledger.AnswerScore, Score: 2, Dist: []ledger.Slice{
				{Option: "0", P: 0.01}, {Option: "1", P: 0.08}, {Option: "2", P: 0.88}, {Option: "3", P: 0.03},
			}},
			{Question: "user_requested", Kind: ledger.AnswerNoul, Noul: 0.11},
		},
		Reason: &ledger.Reason{
			Question:   "risk",
			Comparison: "risk_ask_at",
			Threshold:  1.5,
			Value:      2,
			Mode:       ledger.ModeShadow,
			ModeReason: &modeReason,
		},
	}
}

func TestTheInterfaceIsHandedNumbersAndNotFormattedText(t *testing.T) {
	decision := gateDecision("write", recordedGateDecision())
	if decision.Verdict != Ask || decision.Tool != "write" {
		t.Fatalf("decision %+v", decision)
	}
	if len(decision.Answers) != 4 {
		t.Fatalf("answers %+v, want the four the row carries", decision.Answers)
	}
	for _, want := range []GateAnswer{
		{Question: "approval", Value: 0.75, Max: 1},
		{Question: "from_untrusted", Value: 0.02, Max: 1},
		{Question: "risk", Value: 2, Max: 3},
		{Question: "user_requested", Value: 0.11, Max: 1},
	} {
		if !slices.Contains(decision.Answers, want) {
			t.Errorf("answer %+v is missing from %+v", want, decision.Answers)
		}
	}
	if decision.Reason.Value != 2 || decision.Reason.Threshold != 1.5 || decision.Reason.Limit != "risk_ask_at" {
		t.Errorf("reason %+v", decision.Reason)
	}
	for _, text := range stringsIn(reflect.ValueOf(decision)) {
		if strings.ContainsAny(text, "0123456789%▓░") {
			t.Errorf("the decision carries formatted text %q", text)
		}
	}
}

func stringsIn(value reflect.Value) []string {
	var out []string
	switch value.Kind() {
	case reflect.String:
		return []string{value.String()}
	case reflect.Struct:
		for index := range value.NumField() {
			out = append(out, stringsIn(value.Field(index))...)
		}
	case reflect.Slice:
		for index := range value.Len() {
			out = append(out, stringsIn(value.Index(index))...)
		}
	}
	return out
}

func TestACallReadsAsIntentAndKeepsTheWholeCommandBehindIt(t *testing.T) {
	long := `cd /mnt/q/code/ephem-sh/bob; for d in internal/* interface/* cmd/*; ` +
		`do n=$(find "$d" -name '*.go' | wc -l); echo "$d $n"; done`
	for _, want := range []struct {
		arguments string
		intent    string
		detail    string
	}{
		{`{"command":"go test ./...","timeout":30}`, "go test ./...", "go test ./..."},
		{`{"command":"` + strings.ReplaceAll(long, `"`, `\"`) + `"}`, "for d in internal/* interface/* cmd/* +3 more", long},
		{`{"path":"internal/turn/loop.go"}`, "internal/turn/loop.go", ""},
		{`{"pattern":"Decide","glob":"*.go"}`, "Decide in *.go", ""},
		{`{"pattern":"Decide"}`, "Decide in " + workingDirectory, ""},
		{`{"task":"rename the judge\n\nkeep the wire","owns":["internal/judge/**"]}`, "rename the judge", "rename the judge\n\nkeep the wire"},
		{`{"task":"rename the judge","mission":"judge rename","owns":["internal/judge/**"]}`, "judge rename", "rename the judge"},
		{`{"handle":"99248324d40bbf11be9cd47093978332","offset":0,"length":512}`, moreOfAStoredResult, ""},
	} {
		intent, detail := callIntent(llm.ToolCall{Arguments: []byte(want.arguments)})
		if intent != want.intent || detail != want.detail {
			t.Errorf("%s reads as %q with %q behind it, want %q and %q", want.arguments, intent, detail, want.intent, want.detail)
		}
	}
}

func TestAResultSaysSomethingOrSaysNothing(t *testing.T) {
	for _, want := range []struct {
		content string
		summary string
	}{
		{"ok  tofu/internal/turn\n", "ok tofu/internal/turn"},
		{"", noOutput},
		{"a\nb\nc\n", "3 lines, 6 bytes"},
		{strings.Repeat("x", 40), "1 line, 40 bytes"},
		{strings.Repeat("a line of it\n", 200), "200 lines, 2.5 KB"},
	} {
		if got := resultSummary(want.content); got != want.summary {
			t.Errorf("a result of %d bytes reads as %q, want %q", len(want.content), got, want.summary)
		}
	}
}

func TestAnOversizeResultNeverPutsTheModelsHandleOnTheScreen(t *testing.T) {
	artifacts, err := turn.NewArtifacts(filepath.Join(t.TempDir(), "artifacts"), true)
	if err != nil {
		t.Fatal(err)
	}
	whole := strings.Repeat("a line of the file it read\n", 460)
	message, handle, err := artifacts.Render("read", nil, whole, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if handle == "" || !strings.Contains(message, "artifact_fetch") {
		t.Fatalf("the turn did not compose a handle preamble, so this test proves nothing:\n%s", message)
	}
	shown := resultSummary(message)
	for _, forbidden := range []string{"artifact_fetch", "not pasted", "holds this result whole", handle} {
		if strings.Contains(shown, forbidden) {
			t.Errorf("the summary the screen draws carries %q, which is written for the model: %q", forbidden, shown)
		}
	}
	if want := "12.1 KB" + storedNote; shown != want {
		t.Errorf("the summary reads %q, want the size of the stored file %q", shown, want)
	}
}

func TestTheDrainTakesEveryQueuedMessageAsOneInOrderAndSaysEachOnce(t *testing.T) {
	h, _ := New(Config{Now: time.Now})
	t.Cleanup(h.Close)
	want := []string{"read the policy first", "and leave the changelog alone", "then say what changed"}
	for _, typed := range want {
		h.Steer(typed)
	}
	var told []string
	taken := h.steering.take(func(event Event) {
		if event.Kind != EventSteered {
			t.Errorf("the drain emitted kind %v, want a steered event", event.Kind)
		}
		told = append(told, event.Text)
	}, 1)
	if joined := strings.Join(want, "\n\n"); !slices.Equal(taken, []string{joined}) || !slices.Equal(told, want) {
		t.Fatalf("the drain took %q and said %q, want one message %q and each said once", taken, told, joined)
	}
	if again := h.steering.take(func(Event) { t.Error("an empty queue said something") }, 1); again != nil {
		t.Fatalf("a second drain took %q from an empty queue", again)
	}
}

func TestNoMessageIsDroppedPastTheChannelAndOrderHolds(t *testing.T) {
	h, _ := New(Config{Now: time.Now})
	t.Cleanup(h.Close)
	var want []string
	for index := range konst.HostSteeringQueue + 6 {
		want = append(want, fmt.Sprintf("message %d", index))
	}
	h.Steer(want[0])
	first := <-h.steering.ready
	for _, typed := range want[1:] {
		h.Steer(typed)
	}
	var told []string
	taken := h.steering.take(func(event Event) { told = append(told, event.Text) }, 1)
	if got := append([]string{first}, told...); !slices.Equal(got, want) || len(taken) != 1 {
		t.Fatalf("%d of %d messages came out, in this order: %q", len(got), len(want), got)
	}
}

func TestDroppingTheSteeringEmptiesTheOverflowToo(t *testing.T) {
	h, _ := New(Config{Now: time.Now})
	t.Cleanup(h.Close)
	for index := range konst.HostSteeringQueue + 2 {
		h.Steer(fmt.Sprint(index))
	}
	h.DropSteering()
	if taken := h.steering.take(func(Event) {}, 1); taken != nil {
		t.Fatalf("a dropped queue still gave the next turn %q", taken)
	}
	h.Steer("after the stop")
	if taken := h.steering.take(func(Event) {}, 1); !slices.Equal(taken, []string{"after the stop"}) {
		t.Fatalf("the first message after a drop came out as %q", taken)
	}
}

func TestUnsteerTakesBackOneMessageAndOnlyWhileItWaits(t *testing.T) {
	h, _ := New(Config{Now: time.Now})
	t.Cleanup(h.Close)
	for _, typed := range []string{"one", "two", "two", "three"} {
		h.Steer(typed)
	}
	if !h.Unsteer("two") {
		t.Fatal("a waiting message could not be taken back")
	}
	if h.Unsteer("four") {
		t.Fatal("a message never queued was taken back")
	}
	if taken := h.steering.take(func(Event) {}, 1); !slices.Equal(taken, []string{"one\n\ntwo\n\nthree"}) {
		t.Fatalf("after taking one two back the lead gets %q", taken)
	}
	if h.Unsteer("one") {
		t.Fatal("a message the lead already took was taken back")
	}
}

func TestThePlaceAGrantCoversIsTheToolAndItsSubject(t *testing.T) {
	for _, one := range []struct {
		tool  string
		args  string
		place string
	}{
		{"write", `{"path":"note.txt","content":"a"}`, "write note.txt"},
		{"read", `{"path":"note.txt"}`, "read note.txt"},
		{"bash", `{"command":"git push --force origin main"}`, "bash git push --force"},
		{"bash", `{"command":"git push --force origin develop"}`, "bash git push --force"},
		{"bash", `{"command":"rm -rf build"}`, "bash rm -rf build"},
		{"glob", `{"pattern":"*.go"}`, `glob {"pattern":"*.go"}`},
	} {
		got := askedPlace(turn.GateRequest{Tool: one.tool, Args: json.RawMessage(one.args)})
		if got != one.place {
			t.Errorf("%s %s is the place %q, want %q", one.tool, one.args, got, one.place)
		}
	}
}

func TestEveryOutcomeClosesTheTurnInWordsAndNeverInItsEnumName(t *testing.T) {
	want := map[turn.Outcome]string{
		turn.OutcomeUnset:               "finished in",
		turn.OutcomeForked:              "finished in",
		turn.OutcomeStopped:             "cooked for",
		turn.OutcomeStepCap:             "stopped at the step cap after",
		turn.OutcomeDecisionCap:         "stopped at the decision cap after",
		turn.OutcomeError:               "failed after",
		turn.OutcomeTruncated:           "stopped on a reply it could not finish, after",
		turn.OutcomeRetiredCostCap:      "stopped at a cap this build no longer sets, after",
		turn.OutcomeRetiredWallClockCap: "stopped at the wall clock cap after",
		turn.OutcomeLoopGuard:           loopGuardWords(nil) + ", after",
	}
	for _, outcome := range session.AllOutcomes() {
		expected, named := want[outcome]
		if !named {
			t.Fatalf("%s carries no expected closing words in this test, so a new outcome can reach doneWords untested", outcome)
		}
		got, _ := doneWords(outcome, nil)
		if got != expected {
			t.Errorf("%s closes the turn with %q, want %q", outcome, got, expected)
		}
		if strings.Contains(got, "_") {
			t.Errorf("%s closes the turn with its own enum name: %q", outcome, got)
		}
	}
}

func TestVerdictOfNamesEveryLedgerVerdict(t *testing.T) {
	want := map[ledger.Verdict]Verdict{
		ledger.VerdictUnset: Ask,
		ledger.VerdictAllow: Allow,
		ledger.VerdictAsk:   Ask,
		ledger.VerdictDeny:  Deny,
	}
	for _, v := range ledger.AllVerdicts() {
		expected, named := want[v]
		if !named {
			t.Fatalf("%s carries no expected verdict, so a new ledger verdict can reach verdictOf untested", v)
		}
		if got := verdictOf(v); got != expected {
			t.Errorf("verdictOf(%s) = %s, want %s", v, got, expected)
		}
	}
}

func TestTheSubAgentBarFollowsTheCapTheTurnWasGiven(t *testing.T) {
	for _, one := range []struct{ maxSteps, total int }{{0, konst.TurnMaxSteps}, {12, 12}} {
		var sent []Event
		watch := &watcher{
			emit:     func(event Event) { sent = append(sent, event) },
			now:      time.Now,
			maxSteps: one.maxSteps,
			held:     rosterHolding(t, roster.SubAgent{ID: "parent-c1", Mission: "do it", Owns: []string{"x"}}),
		}
		watch.sendSubAgents()
		if got := sent[0].SubAgents[0].Total; got != one.total {
			t.Fatalf("a bar under a cap of %d draws %d steps, want %d", one.maxSteps, got, one.total)
		}
	}
}

func gateRowFixture(t *testing.T) (string, ledger.Row) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	row, err := ledger.NewWriter(dir).Append(ledger.Row{Point: "tool_gate"})
	if err != nil {
		t.Fatalf("writing the row fixture: %v", err)
	}
	return dir, row
}

func TestAwaitPersonWritesTheAnswerOntoTheRowAsAnOutcome(t *testing.T) {
	dir, row := gateRowFixture(t)
	book := &asks{standing: map[string]Answer{}}
	person := awaitPerson(func(event Event) {
		if event.Kind == EventAwaitPerson {
			book.answer(event.ID, AllowedOnce)
		}
	}, book)

	answer, err := person(context.Background(), turn.GateRequest{Tool: "write"}, turn.GateDecision{ID: row.ID})
	if err != nil || answer != turn.PersonAllowedOnce {
		t.Fatalf("person returned %v, %v", answer, err)
	}

	read, ok, err := ledger.NewReader(dir).ByID(row.ID)
	if err != nil || !ok {
		t.Fatalf("reading the row back: ok=%v err=%v", ok, err)
	}
	if read.Outcome == nil || read.Outcome.Kind != turn.OutcomeKindGateAnswer || read.Outcome.Detail != "allow" {
		t.Fatalf("the row's outcome is %+v, want kind %q detail allow", read.Outcome, turn.OutcomeKindGateAnswer)
	}
}

func TestAwaitPersonUnderAnAlreadyGrantedRuleWritesNoOutcome(t *testing.T) {
	dir, row := gateRowFixture(t)
	request := turn.GateRequest{Tool: "write", Args: json.RawMessage(`{"path":"a.txt"}`)}
	person := awaitPerson(func(Event) {}, &asks{standing: map[string]Answer{askedPlace(request): AlwaysHere}})

	answer, err := person(context.Background(), request, turn.GateDecision{ID: row.ID})
	if err != nil || answer != turn.PersonAlwaysHere {
		t.Fatalf("person returned %v, %v", answer, err)
	}

	read, ok, err := ledger.NewReader(dir).ByID(row.ID)
	if err != nil || !ok {
		t.Fatalf("reading the row back: ok=%v err=%v", ok, err)
	}
	if read.Outcome != nil {
		t.Fatalf("a decision nobody was asked about must stay unlabelled, got %+v", read.Outcome)
	}
}

func TestARuleQuestionIsAskedEveryTimeAndNeverFillsTheAlwaysHereCache(t *testing.T) {
	request := turn.GateRequest{Tool: tools.RuleOverride{}.Name(), Args: json.RawMessage(`{"rule":"no_unit_test_after_code","change":"off","question":"A rule stops me: no_unit_test_after_code."}`)}
	book := &asks{standing: map[string]Answer{askedPlace(request): AlwaysHere}}
	next := Denied
	var shown []Event
	person := awaitPerson(func(event Event) {
		shown = append(shown, event)
		if event.Kind == EventAwaitPerson {
			book.answer(event.ID, next)
		}
	}, book)
	if got, err := person(context.Background(), request, turn.GateDecision{Verdict: ledger.VerdictAsk}); err != nil || got != turn.PersonDenied {
		t.Fatalf("a rule question under a granted place answered %v, %v, want the person's no", got, err)
	}
	clear(book.standing)
	next = AlwaysHere
	if got, err := person(context.Background(), request, turn.GateDecision{Verdict: ledger.VerdictAsk}); err != nil || got != turn.PersonAlwaysHere || len(book.standing) > 0 {
		t.Fatalf("everywhere answered %v, %v and left the cache %v, want everywhere and an empty cache", got, err, book.standing)
	}
	asked := slices.ContainsFunc(shown, func(event Event) bool {
		return event.Kind == EventDecision && event.Decision != nil && event.Decision.OverridesRule == overrideAsked
	})
	said := slices.ContainsFunc(shown, func(event Event) bool { return strings.Contains(event.Text, "A rule stops me") })
	if !asked || !said {
		t.Errorf("the person was asked without the rule %v or the question %v: %+v", asked, said, shown)
	}
}

func TestTheLeadsLogCloseErrorReachesTheChatOnceAndATurnsErrorIsNotRepeated(t *testing.T) {
	turnErr := fmt.Errorf("turn: %w", context.Canceled)
	closeErr := errors.New("session: closing the log of sub-1: the disk is full")
	if shown := unreported(errors.Join(turnErr, nil, errors.Join(closeErr)), []error{turnErr}); shown == nil || shown.Error() != closeErr.Error() {
		t.Errorf("the lead loop's error was shown as %v, want the log close error alone", shown)
	}
	if shown := unreported(errors.Join(turnErr), []error{turnErr}); shown != nil {
		t.Errorf("a turn's own error, already in the chat, was shown again as %v", shown)
	}
	if shown := unreported(nil, nil); shown != nil {
		t.Errorf("a loop that ended clean showed %v", shown)
	}
}

func TestEveryPlanStateTheToolHasIsDrawable(t *testing.T) {
	drawn := statedPlan([]tools.PlanItem{
		{Text: "pending", State: tools.PlanPending},
		{Text: "running", State: tools.PlanRunning},
		{Text: "done", State: tools.PlanDone},
		{Text: "dropped", State: tools.PlanDropped},
	})
	want := []PlanItem{
		{Text: "pending", State: PlanPending},
		{Text: "running", State: PlanRunning},
		{Text: "done", State: PlanDone},
		{Text: "dropped", State: PlanDropped},
	}
	if !slices.Equal(drawn, want) {
		t.Fatalf("the view is handed %+v, want %+v", drawn, want)
	}
}

func TestDrawnPlanStateNamesEveryToolPlanState(t *testing.T) {
	want := map[tools.PlanState]PlanState{
		tools.PlanPending: PlanPending,
		tools.PlanRunning: PlanRunning,
		tools.PlanDone:    PlanDone,
		tools.PlanDropped: PlanDropped,
	}
	for _, state := range tools.AllPlanStates() {
		expected, named := want[state]
		if !named {
			t.Fatalf("%s carries no expected drawn state, so a new plan state can reach drawnPlanState untested", state)
		}
		if got := drawnPlanState(state); got != expected {
			t.Fatalf("drawnPlanState(%s) = %v, want %v", state, got, expected)
		}
	}
}
