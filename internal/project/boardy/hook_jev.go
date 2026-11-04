package boardy

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/judge/ledger"
	question "tofu/internal/judge/questions"
	"tofu/internal/models/classifiers"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/platform/project"
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

func jevTicketOf(t Ticket) jevTicket {
	inForce := ""
	if len(t.Acceptance) > 0 {
		inForce = t.Acceptance[len(t.Acceptance)-1]
	}
	return jevTicket{ID: t.ID, Title: t.Title, Type: t.Type, Problem: t.Problem, Scope: t.Scope, Acceptance: inForce}
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
	state := jevState{Ticket: jevTicketOf(s.ticket), Epics: []jevEpic{}}
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
		request.Options = append(request.Options, core.Option{Name: epic.ID, Criteria: strings.TrimSpace(epic.Title + ". " + epic.Text)})
	}
	body, err := json.Marshal(state)
	if err != nil {
		return "", err.Error()
	}
	row := ledger.Row{Point: boardHookPoint, Questions: set.Name, Version: set.QuestionsVersion, StateHash: ledger.HashOf(body), StateBuilder: boardHookBuilder, State: body,
		Verdict: ledger.VerdictAsk, Reason: &ledger.Reason{Question: asked.Name, Comparison: ">= " + strconv.FormatFloat(threshold, 'f', -1, 64), Threshold: threshold, Mode: ledger.ModeEnforced}}
	places, err := project.ResolvePlaces(m.Store.Root)
	if err != nil {
		return "", err.Error()
	}
	client, err := classifiers.Open(m.Store.Root, core.ReadKeyring(), 1)
	var decision core.Decision
	if err == nil {
		decision, err = client.Ask(context.Background(), core.Request{State: state, Questions: []core.Question{request}})
	}
	answer := decision.Answers[asked.Name]
	p := answer.Probabilities[answer.Choice]
	var said string
	if err != nil {
		said = "Jev could not answer, so nothing is set: " + err.Error()
		row.Reason.ModeReason = &said
	} else {
		said = fmt.Sprintf("Jev chose %s at %.2f, and %s sets it at %.2f or above", answer.Choice, p, ref, threshold)
		decision.Record(&row, []core.Question{request}, set.QuestionsVersion)
		row.Reason.Value = p
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
