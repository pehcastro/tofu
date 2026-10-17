package boardy

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/library/questions"
)

const (
	boardHookPoint   = "board_hook"
	boardHookBuilder = "board_hook_state@1"
)

type jevTicket struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Type       Kind   `json:"type"`
	Problem    string `json:"problem"`
	Scope      string `json:"scope"`
	Acceptance string `json:"acceptance"`
}

type jevEpic struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type jevState struct {
	Ticket jevTicket `json:"ticket"`
	Epics  []jevEpic `json:"epics"`
}

func (m Managed) askJev(ref string, s subject) (string, string) {
	epics, err := m.Store.Epics(s.key)
	if err != nil {
		return "", "the epics could not be read: " + err.Error()
	}
	t := s.ticket
	state := jevState{Ticket: jevTicket{ID: t.ID, Title: t.Title, Type: t.Type, Problem: t.Problem, Scope: t.Scope}, Epics: []jevEpic{}}
	if len(t.Acceptance) > 0 {
		state.Ticket.Acceptance = t.Acceptance[len(t.Acceptance)-1]
	}
	for _, epic := range epics {
		if !epic.Done {
			state.Epics = append(state.Epics, jevEpic{ID: epic.ID, Title: epic.Title, Text: epic.Text})
		}
	}
	if len(state.Epics) == 0 {
		return "", "the board has no open epic for Jev to choose from"
	}
	layers, err := question.Layers(questions.Files(), m.Store.Root)
	if err != nil {
		return "", err.Error()
	}
	set, fields, err := question.Resolve(ref, layers)
	if err != nil {
		return "", err.Error()
	}
	if len(set.Questions) != 1 || set.Questions[0].Kind != question.KindChoice {
		return "", fmt.Sprintf("%s asks %d questions, and a board hook asks one choice question", ref, len(set.Questions))
	}
	asked := set.Questions[0]
	at := slices.IndexFunc(fields, func(f question.Field) bool { return f.Path == "thresholds."+asked.Name+"_at" })
	var threshold float64
	if at >= 0 {
		threshold, err = strconv.ParseFloat(fields[at].Value, 64)
	}
	if at < 0 || err != nil || threshold <= 0 || threshold > 1 {
		return "", fmt.Sprintf("%s declares no thresholds.%s_at above 0 and at or below 1", ref, asked.Name)
	}
	request := asked.ToJev()
	for _, epic := range state.Epics {
		request.Options = append(request.Options, jev.Option{Name: epic.ID, Criteria: strings.TrimSpace(epic.Title + ". " + epic.Text)})
	}
	body, err := json.Marshal(state)
	if err != nil {
		return "", err.Error()
	}
	row := ledger.Row{Point: boardHookPoint, Questions: set.Name, Version: set.QuestionsVersion, StateHash: ledger.HashOf(body), StateBuilder: boardHookBuilder, State: body,
		Verdict: ledger.VerdictAsk, Reason: &ledger.Reason{Question: asked.Name, Comparison: ">= " + strconv.FormatFloat(threshold, 'f', -1, 64), Threshold: threshold, Mode: ledger.ModeEnforced}}
	places, err := sys.ResolvePlaces(m.Store.Root)
	if err != nil {
		return "", err.Error()
	}
	decision, err := askTypeSafe(jev.Request{State: state, Questions: []jev.Question{request}})
	answer := decision.Answers[asked.Name]
	p := answer.Probabilities[answer.Choice]
	var said string
	if err != nil {
		said = "Jev could not answer, so nothing is set: " + err.Error()
		row.Reason.ModeReason = &said
	} else {
		said = fmt.Sprintf("Jev chose %s at %.2f, and %s sets it at %.2f or above", answer.Choice, p, ref, threshold)
		row.Build, row.Model, row.RequestID = decision.Build, decision.Alias, cmp.Or(decision.RequestID, decision.TransportID)
		row.LatencyMS, row.Cost, row.Reason.Value = decision.Latency.Milliseconds(), decision.Usage.Cost, p
		dist := make([]ledger.Slice, 0, len(answer.Probabilities))
		for _, option := range slices.Sorted(maps.Keys(answer.Probabilities)) {
			dist = append(dist, ledger.Slice{Option: option, P: answer.Probabilities[option]})
		}
		row.Answers = []ledger.Answer{{Question: asked.Name, Wording: set.QuestionsVersion, Kind: ledger.AnswerChoice, Choice: answer.Choice, Dist: dist}}
		if answer.Choice != asked.Escape && p >= threshold {
			row.Verdict = ledger.VerdictAllow
		}
	}
	if _, err := ledger.NewWriter(places.Log).Append(row); err != nil {
		return "", "the decision did not reach the ledger, so nothing is set: " + err.Error()
	}
	if row.Verdict != ledger.VerdictAllow {
		return "", said
	}
	return answer.Choice, said
}

func askTypeSafe(request jev.Request) (jev.Decision, error) {
	key, err := jev.KeyFor(sys.CredentialFileName, jev.TypeSafeVariable)
	if err != nil {
		return jev.Decision{}, err
	}
	wire, err := typesafe.New(typesafe.Config{Key: key, Transport: transport.Config{
		AttemptTimeout: konst.JudgeTimeoutMillis * time.Millisecond,
		Retries:        konst.JudgeRetries,
		Backoff:        konst.JudgeBackoffMillis * time.Millisecond,
		Concurrency:    1,
	}})
	if err != nil {
		return jev.Decision{}, err
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		return jev.Decision{}, err
	}
	return client.Ask(context.Background(), request)
}
