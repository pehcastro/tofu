package loop

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"sync"

	"tofu/internal/agent/context/recall"
	"tofu/internal/judge/ledger"
	question "tofu/internal/judge/questions"
	"tofu/internal/konst"
	"tofu/internal/models/classifiers"
	"tofu/internal/models/classifiers/core"
	"tofu/library/questions"
)

const (
	standingOrderRef     = "standing_order@1"
	standingOrderPoint   = "standing_order"
	standingOrderName    = "standing"
	standingOrderBuilder = "standing_order_state@1"
)

type standingJudge struct {
	client    *core.Client
	unasked   error
	set       question.Set
	asked     core.Question
	threshold float64
	ledger    *ledger.Writer
	notify    func(string)
	mu        sync.Mutex
	decided   map[string]bool
}

func newStandingJudge(config Config, route classifiers.Opener) *standingJudge {
	judge := &standingJudge{ledger: ledger.NewWriter(config.Places.Log), notify: config.Notify, decided: map[string]bool{}}
	judge.unasked = judge.open(config.Project, route)
	return judge
}

func (j *standingJudge) open(project string, route classifiers.Opener) error {
	layers, err := question.Layers(questions.Files(), project)
	if err != nil {
		return err
	}
	set, fields, err := question.Resolve(standingOrderRef, layers)
	if err != nil {
		return err
	}
	asked, found := set.Question(standingOrderName)
	at := slices.IndexFunc(fields, func(f question.Field) bool { return f.Path == "thresholds."+standingOrderName+"_at" })
	if !found || at < 0 {
		return errors.New(standingOrderRef + " names no " + standingOrderName + " question or no thresholds." + standingOrderName + "_at")
	}
	if j.threshold, err = strconv.ParseFloat(fields[at].Value, 64); err != nil {
		return err
	}
	j.set, j.asked = set, asked.ToJev()
	j.client, err = route(konst.CarryStandingJudged)
	return err
}

func (j *standingJudge) orders(sentences []string) []bool {
	kept := make([]bool, len(sentences))
	var asking sync.WaitGroup
	for i, sentence := range sentences {
		asking.Go(func() { kept[i] = j.order(sentence) })
	}
	asking.Wait()
	return kept
}

func (j *standingJudge) order(sentence string) bool {
	j.mu.Lock()
	kept, done := j.decided[sentence]
	j.mu.Unlock()
	if done {
		return kept
	}
	body, _ := json.Marshal(struct {
		Sentence string `json:"sentence"`
	}{sentence})
	kept = recall.StandingByWords(sentence)
	row := ledger.Row{Point: standingOrderPoint, Questions: j.set.Name, Version: j.set.QuestionsVersion, StateHash: ledger.HashOf(body), StateBuilder: standingOrderBuilder, State: body,
		Reason: &ledger.Reason{Question: standingOrderName, Comparison: ">= " + strconv.FormatFloat(j.threshold, 'f', -1, 64), Threshold: j.threshold, Mode: ledger.ModeEnforced}}
	if j.unasked != nil {
		why := "jev cannot be asked, so the word list decided: " + j.unasked.Error()
		row.Reason.ModeReason = &why
	} else if decision, err := j.client.Ask(context.Background(), core.Request{State: body, Questions: []core.Question{j.asked}}); err != nil {
		why := "jev could not answer, so the word list decided: " + err.Error()
		row.Reason.ModeReason = &why
	} else {
		decision.Record(&row, []core.Question{j.asked}, j.set.QuestionsVersion)
		row.Reason.Value = row.Answers[0].Noul
		kept = row.Reason.Value >= j.threshold
	}
	row.Verdict = ledger.VerdictDeny
	if kept {
		row.Verdict = ledger.VerdictAllow
	}
	j.mu.Lock()
	j.decided[sentence] = kept
	_, err := j.ledger.Append(row)
	j.mu.Unlock()
	if err != nil && j.notify != nil {
		j.notify("a standing order decision was not written to the ledger: " + err.Error())
	}
	return kept
}
