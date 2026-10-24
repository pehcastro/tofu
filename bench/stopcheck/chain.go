package stopcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	"tofu/internal/konst"
)

const (
	chainQuestions = "stop_check_chain"
	chainVersion   = 1
	chainEvidence  = 3
	chainNext      = "next"
	chainUnsureAt  = 0.5
)

const (
	nextContinue = "continue"
	nextAsk      = "ask_person"
	nextStop     = "stop"
)

var chainQuestion = jev.Question{
	ID:   chainNext,
	Kind: jev.QuestionChoice,
	Instructions: "A sub-agent was handed `brief` and has taken the steps in `recent_steps`. " +
		"`last_tool_results` holds what its last three tool calls returned: the tool, the command, " +
		"the exit code, any error and how many bytes came back. Read the brief against that evidence " +
		"and choose what the harness should do with the turn right now.",
	Options: []jev.Option{
		{Name: nextContinue, Criteria: "Part of the brief is still undone and the latest results show the turn moving toward it, so another step is worth taking."},
		{Name: nextAsk, Criteria: "The turn cannot settle the brief on its own: the results show it blocked on a choice or a fact only the person has."},
		{Name: nextStop, Criteria: "Another step will add nothing: the brief is carried out and reported, the turn is repeating itself, or the step was empty."},
	},
}

type toolResult struct {
	Step        int    `json:"step"`
	Tool        string `json:"tool"`
	Command     string `json:"command"`
	ExitCode    *int   `json:"exit_code"`
	Error       string `json:"error"`
	ResultBytes int64  `json:"result_bytes"`
}

type chainState struct {
	state.StopCheckState
	Brief           string       `json:"brief"`
	LastToolResults []toolResult `json:"last_tool_results"`
}

type Chain struct {
	Taken     bool
	Choice    string
	Answer    Answer
	RowID     string
	Replayed  bool
	LatencyMS int64
	Cost      float64
	Charge    ledger.ChargeSource
}

func (b Battery) unsure(answers map[string]float64) bool {
	bothHigh := answers["stalled"] >= chainUnsureAt && answers["work_remains"] >= chainUnsureAt
	nearAsk := math.Abs(answers["stop_pressure"]-b.pol.Thresholds.RiskAskAt) <= konst.ThresholdDeadBand
	return bothHigh || nearAsk
}

func lastToolResults(turn Turn, at int) []toolResult {
	results := []toolResult{}
	for i := at; i >= 0 && len(results) < chainEvidence; i-- {
		calls := turn.Steps[i].Calls
		for j := len(calls) - 1; j >= 0 && len(results) < chainEvidence; j-- {
			c := calls[j]
			results = append(results, toolResult{
				Step: turn.Steps[i].Index, Tool: c.Tool, Command: c.Command,
				ExitCode: c.ExitCode, Error: c.Error, ResultBytes: c.ResultBytes,
			})
		}
	}
	return results
}

func readNext(choice string) (Answer, ledger.Verdict, error) {
	switch choice {
	case nextContinue:
		return Continue, ledger.VerdictAllow, nil
	case nextAsk:
		return Stop, ledger.VerdictAsk, nil
	case nextStop:
		return Stop, ledger.VerdictDeny, nil
	}
	return "", ledger.VerdictUnset, fmt.Errorf("stopcheck: the chain answered %q, which is not one of its options", choice)
}

func (b Battery) chain(ctx context.Context, turn Turn, at int) (Chain, error) {
	first, _, err := state.BuildStopCheck(StateAt(turn, at))
	if err != nil {
		return Chain{}, err
	}
	var built chainState
	if err := json.Unmarshal(first, &built.StopCheckState); err != nil {
		return Chain{}, err
	}
	built.Brief, built.LastToolResults = turn.Task, lastToolResults(turn, at)
	raw, err := ledger.Canonical(built)
	if err != nil {
		return Chain{}, err
	}
	request := ledger.Request{State: json.RawMessage(raw), Questions: chainQuestions, Model: b.model, Version: chainVersion}
	key, err := b.cache.Key(request)
	if err != nil {
		return Chain{}, err
	}
	entry, hit, err := b.cache.Load(key)
	if err != nil {
		return Chain{}, err
	}
	out := Chain{Taken: true, Replayed: hit}
	row := ledger.Row{
		Bench: BenchName, Point: chainQuestions, Questions: chainQuestions, Version: chainVersion,
		Model: b.model, TurnID: turn.ID,
	}
	if hit {
		row.Answers, row.Build, row.RequestID, row.ReplayOf = entry.Answers, entry.Build, entry.RequestID, entry.RowID
		measured, present, err := b.recorded.ByID(entry.RowID)
		if err != nil {
			return Chain{}, err
		}
		if present {
			out.LatencyMS, out.Cost = measured.LatencyMS, measured.Cost
		}
	} else {
		decision, err := b.client.Ask(ctx, jev.Request{State: json.RawMessage(raw), Questions: []jev.Question{chainQuestion}})
		if err != nil {
			return Chain{}, err
		}
		row.Answers = toLedgerAnswers(chainVersion, decision.Answers)
		row.Build, row.RequestID = decision.Build, decision.RequestID
		row.LatencyMS, row.Cost = decision.Latency.Milliseconds(), decision.Charge.USD
		out.Charge = decision.Charge.Source
		out.LatencyMS, out.Cost = row.LatencyMS, row.Cost
	}
	if len(row.Answers) != 1 || row.Answers[0].Kind != ledger.AnswerChoice {
		return Chain{}, fmt.Errorf("stopcheck: the chain asked one choice and got %+v", row.Answers)
	}
	out.Choice = row.Answers[0].Choice
	if out.Answer, row.Verdict, err = readNext(out.Choice); err != nil {
		return Chain{}, err
	}
	if row.StateHash, err = ledger.Hash(json.RawMessage(raw)); err != nil {
		return Chain{}, err
	}
	written, err := b.writer.Append(row)
	if err != nil {
		return Chain{}, err
	}
	out.RowID = written.ID
	if !hit {
		stored := ledger.Entry{RowID: written.ID, Build: written.Build, RequestID: written.RequestID, Answers: row.Answers}
		if err := b.cache.Store(key, request, stored); err != nil {
			return Chain{}, err
		}
	}
	return out, nil
}
