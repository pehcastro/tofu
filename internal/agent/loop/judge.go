package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"tofu/internal/judge/ledger"
	question "tofu/internal/judge/questions"
	"tofu/internal/models/classifiers"
	"tofu/internal/models/classifiers/core"
	"tofu/library/questions"
)

type questionAt struct {
	ref, name, builder string
	onReach            ledger.Verdict
	unanswered         string
}

func newSteerJudge(route classifiers.Opener, project, log string) (*oneQuestion, error) {
	return newOneQuestion(questionAt{ref: "steer_answered@1", name: "answered", builder: "steer_answered_state@1", onReach: ledger.VerdictAllow,
		unanswered: "jev could not answer, so a reply with text counts as the answer: "}, route, project, log)
}

func newOpenJudge(route classifiers.Opener, project, log string) (*oneQuestion, error) {
	return newOneQuestion(questionAt{ref: "request_open@3", name: "open", builder: "request_open_state@2", onReach: ledger.VerdictAsk,
		unanswered: "jev could not answer, so the request is not restated and the lead starts on it: "}, route, project, log)
}

type oneQuestion struct {
	questionAt
	client    *core.Client
	set       question.Set
	asked     core.Question
	threshold float64
	ledger    *ledger.Writer
}

func newOneQuestion(at questionAt, route classifiers.Opener, project, log string) (*oneQuestion, error) {
	layers, err := question.Layers(questions.Files(), project)
	if err != nil {
		return nil, err
	}
	set, fields, err := question.Resolve(at.ref, layers)
	if err != nil {
		return nil, err
	}
	asked, found := set.Question(at.name)
	field := slices.IndexFunc(fields, func(f question.Field) bool { return f.Path == "thresholds."+at.name+"_at" })
	if !found || field < 0 {
		return nil, fmt.Errorf("%s names no %s question or no thresholds.%s_at", at.ref, at.name, at.name)
	}
	threshold, err := strconv.ParseFloat(fields[field].Value, 64)
	if err != nil {
		return nil, err
	}
	client, err := route(1)
	if err != nil {
		return nil, err
	}
	return &oneQuestion{questionAt: at, client: client, set: set, asked: asked.ToJev(), threshold: threshold, ledger: ledger.NewWriter(log)}, nil
}

func (j *oneQuestion) reached(ctx context.Context, session string, state any) (bool, error) {
	body, err := json.Marshal(state)
	if err != nil {
		return false, err
	}
	asked := []core.Question{j.asked}
	row := ledger.Row{Point: j.set.Name, Questions: j.set.Name, Version: j.set.QuestionsVersion, StateHash: ledger.HashOf(body), StateBuilder: j.builder, State: body, SessionID: session,
		Verdict: ledger.VerdictAsk, Reason: &ledger.Reason{Question: j.name, Comparison: ">= " + strconv.FormatFloat(j.threshold, 'f', -1, 64), Threshold: j.threshold, Mode: ledger.ModeEnforced}}
	decision, asking := j.client.Ask(ctx, core.Request{State: json.RawMessage(body), Questions: asked})
	reached := false
	if asking != nil {
		failed := j.unanswered + asking.Error()
		row.Reason.ModeReason = &failed
	} else {
		decision.Record(&row, asked, j.set.QuestionsVersion)
		row.Reason.Value = row.Answers[0].Noul
		reached = row.Reason.Value >= j.threshold
	}
	if reached == (j.onReach == ledger.VerdictAllow) {
		row.Verdict = ledger.VerdictAllow
	}
	_, unwritten := j.ledger.Append(row)
	return reached, errors.Join(asking, unwritten)
}
