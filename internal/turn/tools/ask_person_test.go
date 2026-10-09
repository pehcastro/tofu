package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

type askedAnswer struct {
	ID     string   `json:"id"`
	Status string   `json:"status"`
	Chosen []string `json:"chosen"`
	Note   string   `json:"note"`
}

type askedResult struct {
	Outcome string        `json:"outcome"`
	Answers []askedAnswer `json:"answers"`
	Next    string        `json:"next"`
}

const twoLibraries = `{"questions":[{"id":"lib","header":"HTTP client","question":"Which HTTP client should the fetcher use?","type":"choice",
"options":[{"label":"net/http","description":"standard library, no dependency"},{"label":"resty","description":"retries built in, one dependency"}],"recommended":0}]}`

func personSays(answer turn.PersonAnswer, err error) turn.Person {
	return func(context.Context, turn.GateRequest, turn.GateDecision) (turn.PersonAnswer, error) {
		return answer, err
	}
}

func asked(t *testing.T, tool tools.AskPerson, args string) askedResult {
	t.Helper()
	result, err := tool.Run(t.Context(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("ask_person refused %s: %v", args, err)
	}
	var read askedResult
	if err := json.Unmarshal([]byte(result.Content), &read); err != nil {
		t.Fatalf("the result is not the answer shape: %v\n%s", err, result.Content)
	}
	return read
}

func TestAskPersonRefusesAQuestionSetOutsideTheShape(t *testing.T) {
	option := func(label string) string { return `{"label":"` + label + `","description":"d"}` }
	question := func(id, header, kind, options, recommended string) string {
		return `{"id":"` + id + `","header":"` + header + `","question":"q?","type":"` + kind + `","options":[` + options + `]` + recommended + `}`
	}
	two := option("a") + "," + option("b")
	for _, c := range []struct{ name, args, says string }{
		{"no question", `{"questions":[]}`, "1 to 4"},
		{"five questions", `{"questions":[` + strings.Repeat(question("x", "h", "choice", two, `,"recommended":0`)+",", 4) + question("y", "h", "choice", two, `,"recommended":0`) + `]}`, "1 to 4"},
		{"two ids alike", `{"questions":[` + question("x", "h", "choice", two, `,"recommended":0`) + "," + question("x", "h", "choice", two, `,"recommended":0`) + `]}`, "twice"},
		{"no id", `{"questions":[` + question("", "h", "choice", two, `,"recommended":0`) + `]}`, "id"},
		{"a header of thirteen", `{"questions":[` + question("x", "abcdefghijklm", "choice", two, `,"recommended":0`) + `]}`, "header"},
		{"one option", `{"questions":[` + question("x", "h", "choice", option("a"), `,"recommended":0`) + `]}`, "2 to 4"},
		{"five options", `{"questions":[` + question("x", "h", "multi", two+","+two+","+option("c"), `,"recommended":0`) + `]}`, "2 to 4"},
		{"a label of six words", `{"questions":[` + question("x", "h", "choice", option("one two three four five six")+","+option("b"), `,"recommended":0`) + `]}`, "1 to 5 words"},
		{"an empty label", `{"questions":[` + question("x", "h", "choice", option(" ")+","+option("b"), `,"recommended":0`) + `]}`, "1 to 5 words"},
		{"the model writes other", `{"questions":[` + question("x", "h", "choice", option("Other")+","+option("b"), `,"recommended":0`) + `]}`, "other"},
		{"no recommended", `{"questions":[` + question("x", "h", "choice", two, "") + `]}`, "recommended"},
		{"recommended past the end", `{"questions":[` + question("x", "h", "choice", two, `,"recommended":2`) + `]}`, "recommended"},
		{"recommended below zero", `{"questions":[` + question("x", "h", "choice", two, `,"recommended":-1`) + `]}`, "recommended"},
		{"an unknown type", `{"questions":[` + question("x", "h", "poll", two, `,"recommended":0`) + `]}`, "type"},
		{"text with options", `{"questions":[` + question("x", "h", "text", two, "") + `]}`, "text"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tool := tools.AskPerson{Person: personSays(turn.PersonAllowedOnce, nil), Auto: func() bool { return false }}
			_, err := tool.Run(t.Context(), json.RawMessage(c.args))
			if err == nil || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("got %v, want a refusal saying %q", err, c.says)
			}
		})
	}
}

func TestAskPersonTakesATwelveRuneHeaderThatIsLongerInBytes(t *testing.T) {
	tool := tools.AskPerson{Person: personSays(turn.PersonAllowedOnce, nil), Auto: func() bool { return false }}
	got := asked(t, tool, strings.Replace(twoLibraries, "HTTP client", "clientè HTTP", 1))
	if got.Outcome != "submitted" {
		t.Fatalf("outcome %q, want submitted", got.Outcome)
	}
}

func TestAskPersonSaysWhatHappenedToEachQuestion(t *testing.T) {
	yesno := `{"questions":[{"id":"push","header":"Push","question":"Push the branch now?","type":"yesno","recommended":1},
{"id":"name","header":"Name","question":"What should the module be called?","type":"text"}]}`
	for _, c := range []struct {
		name     string
		person   turn.Person
		args     string
		outcome  string
		statuses []string
		chosen   [][]string
	}{
		{"allowed", personSays(turn.PersonAllowedOnce, nil), twoLibraries, "submitted", []string{"answered"}, [][]string{{"net/http"}}},
		{"always here", personSays(turn.PersonAlwaysHere, nil), twoLibraries, "submitted", []string{"answered"}, [][]string{{"net/http"}}},
		{"denied", personSays(turn.PersonDenied, nil), twoLibraries, "submitted", []string{"skipped"}, [][]string{nil}},
		{"yes or no and a typed one", personSays(turn.PersonAllowedOnce, nil), yesno, "submitted", []string{"answered", "unanswered"}, [][]string{{"no"}, nil}},
		{"nobody could be asked", personSays(turn.PersonDenied, errors.New("the client declared no questions")), twoLibraries, "undelivered", []string{"unanswered"}, [][]string{{"net/http"}}},
		{"no person at all", nil, twoLibraries, "undelivered", []string{"unanswered"}, [][]string{{"net/http"}}},
		{"stopped while it waited", personSays(turn.PersonDenied, context.Canceled), twoLibraries, "cancelled", []string{"unanswered"}, [][]string{{"net/http"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := asked(t, tools.AskPerson{Person: c.person, Auto: func() bool { return false }}, c.args)
			if got.Outcome != c.outcome || len(got.Answers) != len(c.statuses) {
				t.Fatalf("got %+v, want outcome %s and %d answers", got, c.outcome, len(c.statuses))
			}
			for i, answer := range got.Answers {
				if answer.Status != c.statuses[i] || strings.Join(answer.Chosen, ",") != strings.Join(c.chosen[i], ",") {
					t.Errorf("answer %d is %+v, want %s choosing %v", i, answer, c.statuses[i], c.chosen[i])
				}
			}
			if c.outcome != "submitted" && !strings.Contains(got.Next, "recommended") {
				t.Errorf("next is %q, and on %s the lead takes the recommended option", got.Next, c.outcome)
			}
		})
	}
}

func TestAskPersonInAutoNeverStopsTheWorkAndALaterAnswerIsASteer(t *testing.T) {
	release := make(chan struct{})
	slow := func(ctx context.Context, _ turn.GateRequest, _ turn.GateDecision) (turn.PersonAnswer, error) {
		<-release
		return turn.PersonAllowedOnce, nil
	}
	inbox := turn.NewInbox()
	tool := tools.AskPerson{Person: slow, Auto: func() bool { return true }, Wait: 20 * time.Millisecond, Inbox: inbox}
	result, err := tool.Run(t.Context(), json.RawMessage(twoLibraries))
	if err != nil || !strings.Contains(result.Content, "comes as a message") {
		t.Fatalf("got %q, %v, want the call back at once saying the answer comes as a message", result.Content, err)
	}
	timedOut := inboxSays(t, inbox)
	if !strings.Contains(timedOut, `"timed_out"`) || !strings.Contains(timedOut, "auto-selected after timeout") || !strings.Contains(timedOut, "net/http") {
		t.Fatalf("after the wait the lead read %q, want timed_out with net/http auto-selected", timedOut)
	}
	close(release)
	if later := inboxSays(t, inbox); !strings.Contains(later, `"submitted"`) || !strings.Contains(later, `"lib"`) {
		t.Fatalf("the later answer reached the lead as %q, want submitted naming lib", later)
	}
}

func inboxSays(t *testing.T, inbox *turn.Inbox) string {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if taken := inbox.Take(); len(taken) > 0 {
			return strings.Join(taken, "\n")
		}
	}
	t.Fatal("nothing reached the lead's inbox")
	return ""
}

func TestAskPersonInAutoTakesAnAnswerThatArrivesInTime(t *testing.T) {
	inbox := turn.NewInbox()
	tool := tools.AskPerson{Person: personSays(turn.PersonAllowedOnce, nil), Auto: func() bool { return true }, Wait: time.Minute, Inbox: inbox}
	if _, err := tool.Run(t.Context(), json.RawMessage(twoLibraries)); err != nil {
		t.Fatal(err)
	}
	if said := inboxSays(t, inbox); !strings.Contains(said, `"submitted"`) || strings.Contains(said, "timed_out") {
		t.Fatalf("got %q, want submitted", said)
	}
}

func TestAskPersonInAskModeWaitsPastTheAutoWait(t *testing.T) {
	slow := func(context.Context, turn.GateRequest, turn.GateDecision) (turn.PersonAnswer, error) {
		time.Sleep(60 * time.Millisecond)
		return turn.PersonAllowedOnce, nil
	}
	tool := tools.AskPerson{Person: slow, Auto: func() bool { return false }, Wait: 10 * time.Millisecond}
	if got := asked(t, tool, twoLibraries); got.Outcome != "submitted" {
		t.Fatalf("got %+v, want submitted, since ask mode has no timeout", got)
	}
}

func TestAskPersonSendsNoLateAnswerAfterAStop(t *testing.T) {
	ctx, stop := context.WithCancel(t.Context())
	waits := func(ctx context.Context, _ turn.GateRequest, _ turn.GateDecision) (turn.PersonAnswer, error) {
		<-ctx.Done()
		return turn.PersonDenied, ctx.Err()
	}
	inbox := turn.NewInbox()
	tool := tools.AskPerson{Person: waits, Auto: func() bool { return true }, Wait: time.Minute, Inbox: inbox}
	if _, err := tool.Run(ctx, json.RawMessage(twoLibraries)); err != nil {
		t.Fatal(err)
	}
	stop()
	time.Sleep(100 * time.Millisecond)
	if taken := inbox.Take(); len(taken) > 0 {
		t.Fatalf("a stopped question reached the lead as %q", taken)
	}
}

const threeKinds = `{"questions":[{"id":"lib","header":"HTTP client","question":"Which HTTP client?","type":"choice",
"options":[{"label":"net/http","description":"standard"},{"label":"resty","description":"retries","preview":"r := resty.New()"}],"recommended":0},
{"id":"push","header":"Push","question":"Push when done?","type":"yesno","recommended":1},
{"id":"name","header":"Name","question":"What is the module called?","type":"text"}]}`

func TestAskPersonThroughTheFormSaysWhatWasChosenTypedAndSkipped(t *testing.T) {
	var shown []turn.PersonQuestion
	var waited time.Duration
	form := func(_ context.Context, questions []turn.PersonQuestion, wait time.Duration) ([]turn.PersonReply, error) {
		shown, waited = questions, wait
		return []turn.PersonReply{{ID: "lib", Chosen: []string{"resty"}}, {ID: "push", Chosen: []string{}}, {ID: "name", Chosen: []string{}, Text: "fetchx"}}, nil
	}
	tool := tools.AskPerson{Person: personSays(turn.PersonDenied, nil), Auto: func() bool { return false }, Wait: time.Minute}
	result, err := tool.Run(turn.WithPersonForm(t.Context(), form), json.RawMessage(threeKinds))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Outcome string `json:"outcome"`
		Answers []struct {
			ID, Status, Text string
			Chosen           []string
		} `json:"answers"`
	}
	if err := json.Unmarshal([]byte(result.Content), &got); err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "submitted" || len(got.Answers) != 3 {
		t.Fatalf("got %s", result.Content)
	}
	want := []string{"answered resty ", "skipped  ", "answered  fetchx"}
	for i, answer := range got.Answers {
		if line := answer.Status + " " + strings.Join(answer.Chosen, ",") + " " + answer.Text; line != want[i] {
			t.Errorf("answer %d reads %q, want %q", i, line, want[i])
		}
	}
	if waited != 0 {
		t.Errorf("ask mode handed the form a wait of %s, want none", waited)
	}
	if len(shown) != 3 || len(shown[1].Options) != 2 || shown[1].Options[0].Label != "yes" || shown[0].Options[1].Preview == "" {
		t.Fatalf("the form was shown %+v, want yes and no for the yesno and the preview kept", shown)
	}
}

func TestAskPersonFormOutcomesWhenNobodyChooses(t *testing.T) {
	for _, c := range []struct {
		name    string
		err     error
		outcome string
	}{
		{"esc", turn.QuestionDismissed{}, "cancelled"},
		{"a client without questions", turn.QuestionUndelivered{Why: "the client did not declare questions"}, "undelivered"},
	} {
		t.Run(c.name, func(t *testing.T) {
			form := func(context.Context, []turn.PersonQuestion, time.Duration) ([]turn.PersonReply, error) {
				return nil, c.err
			}
			tool := tools.AskPerson{Person: personSays(turn.PersonAllowedOnce, nil), Auto: func() bool { return false }}
			result, err := tool.Run(turn.WithPersonForm(t.Context(), form), json.RawMessage(twoLibraries))
			if err != nil || !strings.Contains(result.Content, `"outcome":"`+c.outcome+`"`) || !strings.Contains(result.Content, `"chosen":["net/http"]`) {
				t.Fatalf("got %q, %v, want %s with the recommended standing", result.Content, err, c.outcome)
			}
		})
	}
}

func TestAskPersonInAutoKeepsTheFormOpenPastTheWait(t *testing.T) {
	release := make(chan struct{})
	var waited time.Duration
	form := func(_ context.Context, _ []turn.PersonQuestion, wait time.Duration) ([]turn.PersonReply, error) {
		waited = wait
		<-release
		return []turn.PersonReply{{ID: "lib", Chosen: []string{"resty"}}}, nil
	}
	inbox := turn.NewInbox()
	tool := tools.AskPerson{Person: personSays(turn.PersonDenied, nil), Auto: func() bool { return true }, Wait: 20 * time.Millisecond, Inbox: inbox}
	if _, err := tool.Run(turn.WithPersonForm(t.Context(), form), json.RawMessage(twoLibraries)); err != nil {
		t.Fatal(err)
	}
	if first := inboxSays(t, inbox); !strings.Contains(first, "timed_out") {
		t.Fatalf("first message %q, want timed_out", first)
	}
	close(release)
	if later := inboxSays(t, inbox); !strings.Contains(later, `"chosen":["resty"]`) {
		t.Fatalf("the late answer read %q, want resty", later)
	}
	if waited != 20*time.Millisecond {
		t.Fatalf("the form was told to wait %s", waited)
	}
}
