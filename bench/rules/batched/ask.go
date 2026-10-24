package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"tofu/bench/api"
	"tofu/bench/report"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/konst"
	"tofu/internal/rule"
	"tofu/internal/sys"
)

type outcome struct {
	ID          string             `json:"id"`
	Build       string             `json:"build"`
	Questions   int                `json:"questions"`
	InputTokens int                `json:"input_tokens"`
	Cost        float64            `json:"cost_usd"`
	WallMS      int64              `json:"wall_ms"`
	Answers     map[string]float64 `json:"answers"`
}

type record struct {
	Date       string    `json:"date"`
	Machine    string    `json:"machine"`
	Wire       string    `json:"wire"`
	Credential string    `json:"credential_kind"`
	Model      string    `json:"model_requested"`
	Outcomes   []outcome `json:"outcomes"`
}

func askAll(ctx context.Context, replies []reply, rules []rule.Rule) (record, error) {
	key, err := jev.TypeSafeKey(filepath.Join(sys.SourceRoot(), ".env"))
	if err != nil {
		return record{}, fmt.Errorf("no TypeSafe key for Jev, and this bench asks nothing else: %w", err)
	}
	wire, err := api.NewWire(key)
	if err != nil {
		return record{}, err
	}
	machine, _ := os.Hostname()
	rec := record{Date: time.Now().Format(time.DateOnly), Machine: machine, Wire: typesafe.Name, Credential: "key", Model: wire.Model()}
	for _, r := range replies {
		var questions []jev.Question
		for _, asked := range applicable(rules, r) {
			questions = append(questions, jev.Question{
				ID:           asked.ID,
				Kind:         jev.QuestionNoul,
				Instructions: "The state holds a person's request and the lead agent's finished reply to it. Does the reply break this rule? The rule: " + asked.Text,
				True:         "yes, the reply breaks the rule",
				False:        "no, the reply keeps the rule, or the rule asks nothing of this reply",
			})
		}
		request := jev.Request{State: map[string]string{"request": r.Request, "reply": r.Reply}, Questions: questions}
		body, err := request.Encode(wire.Model())
		if err != nil {
			return record{}, err
		}
		if estimate := jev.EstimateTokens(len(body)); estimate > konst.JudgeRequestTokenCeiling {
			return record{}, fmt.Errorf("%s: the request is about %d tokens, over the %d ceiling, so it is not sent", r.ID, estimate, konst.JudgeRequestTokenCeiling)
		}
		call := api.Ask(ctx, wire, request)
		if call.Err != nil {
			return record{}, fmt.Errorf("%s: %w", r.ID, call.Err)
		}
		answers := map[string]float64{}
		for id, answer := range call.Response.Answers {
			answers[id] = answer.Noul
		}
		rec.Outcomes = append(rec.Outcomes, outcome{
			ID: r.ID, Build: call.Response.Build, Questions: len(questions),
			InputTokens: call.Response.Usage.InputTokens, Cost: call.Response.Usage.Cost,
			WallMS: call.Raw.Latency.Milliseconds(), Answers: answers,
		})
		fmt.Fprintf(os.Stderr, "%s %d questions %d tokens %d ms\n", r.ID, len(questions), call.Response.Usage.InputTokens, call.Raw.Latency.Milliseconds())
	}
	return rec, nil
}

func readRecord(path string) (record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return record{}, err
	}
	var rec record
	return rec, json.Unmarshal(data, &rec)
}

func writeRecord(path string, rec record) error {
	data, err := json.MarshalIndent(rec, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return report.Write(path, append(data, '\n'), 0o644, "rules batched run")
}
