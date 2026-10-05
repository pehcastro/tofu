package browserbench

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/bench/api"
	"tofu/internal/browser"
	"tofu/internal/judge/jev"
	"tofu/internal/sys"
)

var replayAnswers = flag.String("replay", "", "rebuild the report from this recorded answers file, with no network")

func rebuild(t *testing.T, recording Recording, answersPath string) (string, []Reading) {
	t.Helper()
	set, err := Questions()
	if err != nil {
		t.Fatal(err)
	}
	readings, err := Replay(set, corpus(t), recording)
	if err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(filepath.Dir(answersPath), "report-"+recording.Conditions.Date+".md")
	var report strings.Builder
	if err := Write(&report, recording, readings, filepath.Base(answersPath)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, []byte(report.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return reportPath, readings
}

func save(t *testing.T, path string, recording Recording) {
	t.Helper()
	data, err := json.MarshalIndent(recording, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func recorded(t *testing.T, path string) Recording {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var recording Recording
	if err := json.Unmarshal(data, &recording); err != nil {
		t.Fatal(err)
	}
	return recording
}

func TestTheChooserOverTheCorpus(t *testing.T) {
	answersPath := *replayAnswers
	if answersPath == "" {
		if os.Getenv("TOFU_LIVE") != "1" {
			t.Skip("set TOFU_LIVE=1 for the live pass through OpenRouter, or pass -args -replay <answers file> to rebuild its report with no network")
		}
		sys.AllowLiveCredential(t)
		key, err := jev.Key(filepath.Join("..", "..", ".env"))
		if err != nil {
			t.Fatalf("no credential: %v", err)
		}
		wire, err := api.NewWire(key)
		if err != nil {
			t.Fatal(err)
		}
		set, err := Questions()
		if err != nil {
			t.Fatal(err)
		}
		machine, _ := os.Hostname()
		conditions := Conditions{Date: time.Now().Format(time.DateOnly), Machine: machine, Credential: "key"}
		answersPath = "answers-" + conditions.Date + ".json"
		save(t, answersPath, Record(context.Background(), wire, set, corpus(t), conditions))
	}
	reportPath, readings := rebuild(t, recorded(t, answersPath), answersPath)
	correct := 0
	for _, r := range readings {
		if r.Correct {
			correct++
		}
	}
	t.Logf("%d of %d correct, report at %s", correct, len(readings), reportPath)
}

type scripted struct {
	byURL  map[string]Case
	answer map[string]browser.Action
}

func (s scripted) Caps() jev.WireCaps {
	return jev.WireCaps{Name: "scripted", CriteriaKinds: []jev.CriteriaKind{jev.CriteriaString, jev.CriteriaObject}}
}

func (s scripted) Model() string { return "~typesafe/jev-latest" }

func (s scripted) Post(_ context.Context, body []byte) (jev.Raw, error) {
	var posted struct {
		State struct {
			Page struct {
				URL string `json:"url"`
			} `json:"page"`
		} `json:"state"`
		Questions map[string]struct {
			Criteria map[string]json.RawMessage `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(body, &posted); err != nil {
		return jev.Raw{}, err
	}
	c := s.byURL[posted.State.Page.URL]
	want := s.answer[c.ID]
	target := strconv.Itoa(want.Element)
	if want.Op == browser.OpSelect {
		element, _ := c.Page.Element(want.Element)
		for position, option := range element.Options {
			if option.Value == want.Value {
				target = fmt.Sprintf("%d:%d", want.Element, position+1)
			}
		}
	}
	answers := map[string]any{}
	for id, question := range posted.Questions {
		names := make([]string, 0, len(question.Criteria))
		for name := range question.Criteria {
			names = append(names, name)
		}
		sort.Strings(names)
		choice := names[0]
		switch id {
		case "operation":
			choice = want.Op.String()
		case strings.ToLower(want.Op.String()) + "_target":
			choice = target
		}
		answers[id] = map[string]any{"type": "choice", "choice": choice, "confidence": 1, "probabilities": map[string]float64{choice: 1}}
	}
	input := len(body) / 4
	answer, err := json.Marshal(map[string]any{
		"model": "jev-scripted", "provider": "scripted", "id": "scripted-" + c.ID,
		"usage":   map[string]any{"input_tokens": input, "output_tokens": 0, "cost": float64(input) * jevDollarsPerMillionInput / 1e6},
		"answers": answers,
	})
	return jev.Raw{Body: answer, Attempts: 1, Latency: 150 * time.Millisecond}, err
}

func TestTheReportRebuildsFromRecordedAnswersWithEveryField(t *testing.T) {
	cases := corpus(t)
	wire := scripted{byURL: map[string]Case{}, answer: map[string]browser.Action{}}
	for _, c := range cases {
		wire.byURL[c.Page.URL], wire.answer[c.ID] = c, c.Want
	}
	wire.answer["drafts"] = browser.Action{Op: browser.OpClick, Element: 2}
	set, err := Questions()
	if err != nil {
		t.Fatal(err)
	}
	answersPath := filepath.Join(t.TempDir(), "answers-2026-09-28.json")
	save(t, answersPath, Record(context.Background(), wire, set, cases, Conditions{Date: "2026-09-28", Machine: "scripted", Credential: "none"}))

	reportPath, readings := rebuild(t, recorded(t, answersPath), answersPath)
	for _, r := range readings {
		if r.Correct == (r.Case.ID == "drafts") {
			t.Errorf("%s graded %t for %s against %s", r.Case.ID, r.Correct, action(r.Got), action(r.Case.Want))
		}
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	report := string(data)
	fields := []string{
		"Machine: scripted", "Date: 2026-09-28", "Wire: scripted", "Credential kind: none", "Jev build: jev-scripted",
		"answers-2026-09-28.json", "No call was discarded as a warm up",
		"| 8 | 0 | 7 | 88% |", "wall p50", "wall p90", "wall max", "median input tokens", "dollars per decision",
		"against 178 ms", "against 5300", "| off: the turn's model calls `browser_act` | not measured",
		"| drafts | CLICK 1 | CLICK 2 | false |", "| contact | SELECT 3 \"Damaged item\" | SELECT 3 \"Damaged item\" | true |",
		"Adoption check", "within one task", "at most 0.5x", "at most 0.3x",
		"tofu settings set browserDriver $arm", "foreach ($arm in \"goal\", \"steps\")", "flights-search",
	}
	for _, field := range fields {
		if !strings.Contains(report, field) {
			t.Errorf("the report lacks %q", field)
		}
	}
	if strings.Contains(report, "NaN") {
		t.Error("the report prints NaN")
	}
	t.Log(report)
}
