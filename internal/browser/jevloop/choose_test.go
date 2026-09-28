package jevloop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"tofu/internal/browser"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	"tofu/library/questions"
)

type recordedWire struct {
	answer []byte
	fail   error
	posted [][]byte
}

func (w *recordedWire) Caps() jev.WireCaps {
	return jev.WireCaps{Name: "recorded", CriteriaKinds: []jev.CriteriaKind{jev.CriteriaString, jev.CriteriaObject}}
}

func (w *recordedWire) Model() string { return "~typesafe/jev-latest" }

func (w *recordedWire) Post(_ context.Context, body []byte) (jev.Raw, error) {
	w.posted = append(w.posted, body)
	if w.fail != nil {
		return jev.Raw{}, w.fail
	}
	return jev.Raw{Body: w.answer, Attempts: 1, Latency: time.Millisecond}, nil
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func hotelPage(t *testing.T) browser.Page {
	t.Helper()
	page, err := browser.ParsePage(readFixture(t, "hotel.json"))
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func recordedChooser(t *testing.T, wire *recordedWire) Jev {
	t.Helper()
	set, _, err := question.Resolve("browser_step@1", []question.Layer{{Name: "library", Origin: "library/questions", FS: questions.Files()}})
	if err != nil {
		t.Fatal(err)
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatal(err)
	}
	return Jev{Client: client, Set: set}
}

type postedRequest struct {
	State struct {
		Goal     string            `json:"goal"`
		Elements []browser.Element `json:"elements"`
	} `json:"state"`
	Questions map[string]struct {
		Criteria map[string]json.RawMessage `json:"criteria"`
	} `json:"questions"`
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestTheHotelFormAsksOneOpQuestionAndOneTargetPerOpWithCandidates(t *testing.T) {
	wire := &recordedWire{answer: readFixture(t, "hotel_answer.json")}
	choice, err := recordedChooser(t, wire).Choose(context.Background(), "Find a Design stay in Lisbon with free cancellation", hotelPage(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	var posted postedRequest
	if err := json.Unmarshal(wire.posted[0], &posted); err != nil {
		t.Fatal(err)
	}

	if got, want := strings.Join(sortedKeys(posted.Questions), " "), "click_target operation select_target type_text_target"; got != want {
		t.Fatalf("questions %q, want %q", got, want)
	}
	if got, want := strings.Join(sortedKeys(posted.Questions["operation"].Criteria), " "), "BLOCKED CLICK DONE SCROLL_DOWN SELECT TYPE_TEXT WAIT"; got != want {
		t.Fatalf("operation options %q, want %q", got, want)
	}
	if got, want := strings.Join(sortedKeys(posted.Questions["select_target"].Criteria), " "), "6:1 6:2 6:3"; got != want {
		t.Fatalf("select options %q, want %q", got, want)
	}

	for id, asked := range posted.Questions {
		for _, name := range []string{"11", "12"} {
			if _, offered := asked.Criteria[name]; offered {
				t.Errorf("%s offers element %s, a password or hidden input", id, name)
			}
		}
	}
	for _, element := range posted.State.Elements {
		if element.Index == 11 || element.Index == 12 {
			t.Errorf("the state carries element %d, a password or hidden input", element.Index)
		}
	}

	want := browser.Action{Op: browser.OpSelect, Element: 6, Value: "Design"}
	if choice.Action != want {
		t.Fatalf("chose %+v, want %+v, the select_target option at 0.93", choice.Action, want)
	}
	if choice.Decision.Usage.InputTokens != 5312 {
		t.Fatalf("the decision lost its usage: %+v", choice.Decision.Usage)
	}
}

func TestAQuestionOverTheChoiceCeilingIsRefusedBeforeTheWire(t *testing.T) {
	page := hotelPage(t)
	var options []browser.SelectOption
	for i := 0; i < 300; i++ {
		options = append(options, browser.SelectOption{Label: fmt.Sprint("city ", i), Value: fmt.Sprint(i)})
	}
	page.Elements = append(page.Elements, browser.Element{Index: 99, Role: browser.RoleSelect, Label: "City", Options: options})

	wire := &recordedWire{answer: readFixture(t, "hotel_answer.json")}
	_, err := recordedChooser(t, wire).Choose(context.Background(), "goal", page, nil)
	if err == nil {
		t.Fatal("a 303 option select question was sent")
	}
	if len(wire.posted) != 0 {
		t.Fatalf("the wire was called %d times", len(wire.posted))
	}
}

func TestTheStateCarriesOnlyTheLastTenSteps(t *testing.T) {
	var steps []Step
	for i := 1; i <= 12; i++ {
		steps = append(steps, Step{Choice: Choice{Action: browser.Action{Op: browser.OpClick, Element: i}}, Label: fmt.Sprint("step ", i)})
	}
	wire := &recordedWire{answer: readFixture(t, "hotel_answer.json")}
	if _, err := recordedChooser(t, wire).Choose(context.Background(), "goal", hotelPage(t), steps); err != nil {
		t.Fatal(err)
	}
	var posted struct {
		State struct {
			Recent []struct {
				Action string `json:"action"`
			} `json:"recent_actions"`
		} `json:"state"`
	}
	if err := json.Unmarshal(wire.posted[0], &posted); err != nil {
		t.Fatal(err)
	}
	if len(posted.State.Recent) != 10 || posted.State.Recent[0].Action != "step 3" {
		t.Fatalf("recent actions %+v, want steps 3 to 12", posted.State.Recent)
	}
}
