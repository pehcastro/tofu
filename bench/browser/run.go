package browserbench

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tofu/internal/browser"
	"tofu/internal/browser/jevloop"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	libraryquestions "tofu/library/questions"
)

const jevDollarsPerMillionInput = 0.042

type Conditions struct {
	Date       string `json:"date"`
	Machine    string `json:"machine"`
	Credential string `json:"credential"`
}

type Answer struct {
	ID       string          `json:"id"`
	Body     json.RawMessage `json:"body"`
	Attempts int             `json:"attempts"`
	Latency  time.Duration   `json:"latency_ns"`
	Wall     time.Duration   `json:"wall_ns"`
	Error    string          `json:"error,omitempty"`
}

type Recording struct {
	Conditions Conditions   `json:"conditions"`
	Model      string       `json:"model"`
	Caps       jev.WireCaps `json:"caps"`
	Answers    []Answer     `json:"answers"`
}

type Reading struct {
	Case     Case
	Got      browser.Action
	Correct  bool
	Wall     time.Duration
	Latency  time.Duration
	Attempts int
	Input    int
	Output   int
	Reported float64
	Build    string
	Error    string
}

func (r Reading) Dollars() float64 {
	return float64(r.Input) * jevDollarsPerMillionInput / 1e6
}

func Questions() (question.Set, error) {
	set, _, err := question.Resolve("browser_step@1", []question.Layer{{Name: "library", Origin: "library/questions", FS: libraryquestions.Files()}})
	return set, err
}

type recorder struct {
	jev.Wire
	raw jev.Raw
}

func (r *recorder) Post(ctx context.Context, body []byte) (jev.Raw, error) {
	raw, err := r.Wire.Post(ctx, body)
	r.raw = raw
	return raw, err
}

type replayer struct {
	model string
	caps  jev.WireCaps
	raw   jev.Raw
}

func (r replayer) Caps() jev.WireCaps { return r.caps }

func (r replayer) Model() string { return r.model }

func (r replayer) Post(context.Context, []byte) (jev.Raw, error) { return r.raw, nil }

func choose(ctx context.Context, wire jev.Wire, set question.Set, c Case) (jevloop.Choice, time.Duration, error) {
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		return jevloop.Choice{}, 0, err
	}
	started := time.Now()
	choice, err := jevloop.Jev{Client: client, Set: set}.Choose(ctx, c.Goal, c.Page, nil)
	return choice, time.Since(started), err
}

func Record(ctx context.Context, wire jev.Wire, set question.Set, cases []Case, conditions Conditions) Recording {
	recording := Recording{Conditions: conditions, Model: wire.Model(), Caps: wire.Caps()}
	for _, c := range cases {
		tape := &recorder{Wire: wire}
		_, wall, err := choose(ctx, tape, set, c)
		answer := Answer{ID: c.ID, Attempts: tape.raw.Attempts, Latency: tape.raw.Latency, Wall: wall}
		if err != nil {
			answer.Error = err.Error()
		} else {
			answer.Body = tape.raw.Body
		}
		recording.Answers = append(recording.Answers, answer)
	}
	return recording
}

func Replay(set question.Set, cases []Case, recording Recording) ([]Reading, error) {
	answers := map[string]Answer{}
	for _, answer := range recording.Answers {
		answers[answer.ID] = answer
	}
	readings := make([]Reading, 0, len(cases))
	for _, c := range cases {
		answer, recorded := answers[c.ID]
		if !recorded {
			return nil, fmt.Errorf("the recording holds no answer for %s", c.ID)
		}
		reading := Reading{Case: c, Wall: answer.Wall, Latency: answer.Latency, Attempts: answer.Attempts, Error: answer.Error}
		if answer.Error == "" {
			tape := replayer{model: recording.Model, caps: recording.Caps, raw: jev.Raw{Body: answer.Body, Attempts: answer.Attempts, Latency: answer.Latency}}
			choice, _, err := choose(context.Background(), tape, set, c)
			if err != nil {
				return nil, fmt.Errorf("%s no longer replays against its recorded answer: %w", c.ID, err)
			}
			usage := choice.Decision.Usage
			reading.Got, reading.Correct = choice.Action, choice.Action == c.Want
			reading.Input, reading.Output, reading.Reported, reading.Build = usage.InputTokens, usage.OutputTokens, usage.Cost, choice.Decision.Build
		}
		readings = append(readings, reading)
	}
	return readings, nil
}
