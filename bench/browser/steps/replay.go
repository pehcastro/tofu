package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"tofu/internal/browser"
	"tofu/internal/browser/jevloop"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
)

const (
	recordedRecentCeiling = 10
	operationQuestion     = "operation"
)

type recorded struct {
	goal    string
	page    browser.Page
	recent  json.RawMessage
	steps   int
	answers map[string]string
	row     ledger.Row
}

type Replayed struct {
	Goal          string
	Recorded      int
	RecordedDone  bool
	RecordedJevMS int64
	Decisions     int
	JevCalls      int
	JevMS         int64
	Status        jevloop.Status
	Reason        string
}

type tape struct {
	decisions []recorded
	at        int
	posts     int
	jevMS     int64
}

var errTapeAmbiguous = errors.New("the recording cannot say whether this act ran: its recent actions are capped and identical")

func Replay(dir string, set question.Set) ([]Replayed, error) {
	reader := ledger.NewReader(dir)
	var runs [][]recorded
	_, err := reader.Each(ledger.Filter{Point: "browser_step"}, func(row ledger.Row) error {
		raw, err := reader.State(row)
		if err != nil {
			return err
		}
		decision, err := parseRecorded(row, raw)
		if err != nil {
			return fmt.Errorf("%s: %w", row.ID, err)
		}
		last := len(runs) - 1
		if last < 0 || runs[last][0].goal != decision.goal || decision.steps == 0 && runs[last][len(runs[last])-1].steps > 0 {
			runs = append(runs, nil)
			last++
		}
		runs[last] = append(runs[last], decision)
		return nil
	})
	if err != nil {
		return nil, err
	}
	replayed := make([]Replayed, 0, len(runs))
	for _, run := range runs {
		t := &tape{decisions: run}
		client, err := jev.NewClient(jev.Config{Wire: t})
		if err != nil {
			return nil, err
		}
		judge := jevloop.Jev{Client: client, Set: set, Decided: map[string]jevloop.Choice{}}
		result := jevloop.Loop{
			Browser: jevloop.Browser{Snapshot: t.snapshot, Act: t.act},
			Choose:  judge.Choose,
			Write:   func(context.Context, string, browser.Page, browser.Element) (string, error) { return "replayed", nil },
			Actions: konst.BrowserActionCeiling,
		}.Run(context.Background(), run[0].goal)
		var recordedJevMS int64
		for _, decision := range run {
			recordedJevMS += decision.row.LatencyMS
		}
		replayed = append(replayed, Replayed{
			Goal: run[0].goal, Recorded: len(run), RecordedDone: run[len(run)-1].answers[operationQuestion] == browser.OpDone.String(), RecordedJevMS: recordedJevMS,
			Decisions: result.Decisions, JevCalls: t.posts, JevMS: t.jevMS, Status: result.Status, Reason: result.Reason,
		})
	}
	return replayed, nil
}

func parseRecorded(row ledger.Row, raw json.RawMessage) (recorded, error) {
	var state struct {
		Goal string `json:"goal"`
		Page struct {
			URL   string `json:"url"`
			Title string `json:"title"`
			Text  string `json:"text"`
		} `json:"page"`
		Elements      []browser.Element `json:"elements"`
		RecentActions []json.RawMessage `json:"recent_actions"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return recorded{}, err
	}
	recent, err := json.Marshal(state.RecentActions)
	if err != nil {
		return recorded{}, err
	}
	fingerprint, err := ledger.Hash([]any{state.Page.URL, state.Page.Title, state.Page.Text, state.Elements})
	if err != nil {
		return recorded{}, err
	}
	decision := recorded{goal: state.Goal, recent: recent, steps: len(state.RecentActions), answers: map[string]string{}, row: row}
	decision.page = browser.Page{URL: state.Page.URL, Title: state.Page.Title, Text: state.Page.Text, Elements: state.Elements, Fingerprint: fingerprint}
	for _, answer := range row.Answers {
		decision.answers[answer.Question] = answer.Choice
		if answer.Question != operationQuestion {
			continue
		}
		for _, slice := range answer.Dist {
			decision.page.Scroll.Up = decision.page.Scroll.Up || slice.Option == browser.OpScrollUp.String()
			decision.page.Scroll.Down = decision.page.Scroll.Down || slice.Option == browser.OpScrollDown.String()
		}
	}
	return decision, nil
}

func (t *tape) snapshot(context.Context) (browser.Page, error) {
	return t.decisions[t.at].page, nil
}

func (t *tape) act(context.Context, browser.Page, browser.Action) (browser.Stale, error) {
	if t.at+1 == len(t.decisions) {
		return browser.StaleNone, errors.New("the recording ends before this act")
	}
	now, next := t.decisions[t.at], t.decisions[t.at+1]
	capped := now.steps == recordedRecentCeiling
	if capped && string(next.recent) == string(now.recent) {
		return browser.StaleNone, errTapeAmbiguous
	}
	t.at++
	if capped || next.steps > now.steps {
		return browser.StaleNone, nil
	}
	return browser.StaleCovered, nil
}

func (t *tape) Caps() jev.WireCaps {
	return jev.WireCaps{Name: "replay", CriteriaKinds: []jev.CriteriaKind{jev.CriteriaString, jev.CriteriaObject}}
}

func (t *tape) Model() string { return "~typesafe/jev-latest" }

func (t *tape) Post(_ context.Context, body []byte) (jev.Raw, error) {
	var posted struct {
		Questions map[string]struct {
			Criteria map[string]json.RawMessage `json:"criteria"`
		} `json:"questions"`
	}
	t.posts++
	if err := json.Unmarshal(body, &posted); err != nil {
		return jev.Raw{}, err
	}
	decision := t.decisions[t.at]
	t.jevMS += decision.row.LatencyMS
	answers := map[string]any{}
	for id, asked := range posted.Questions {
		names := slices.Sorted(maps.Keys(asked.Criteria))
		choice, answered := decision.answers[id]
		switch {
		case !answered:
			choice = names[0]
		case !slices.Contains(names, choice):
			return jev.Raw{}, fmt.Errorf("%s answered %s with %q, which the rebuilt page does not offer", decision.row.ID, id, choice)
		}
		answers[id] = map[string]any{"type": "choice", "choice": choice, "confidence": 1, "probabilities": map[string]float64{choice: 1}}
	}
	answer, err := json.Marshal(map[string]any{
		"model": decision.row.Build, "provider": "replay", "id": decision.row.RequestID,
		"usage": map[string]any{"cost": decision.row.Cost}, "answers": answers,
	})
	return jev.Raw{Body: answer, Attempts: 1, Latency: time.Duration(decision.row.LatencyMS) * time.Millisecond}, err
}
