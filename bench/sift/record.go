package sift

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
)

const BenchName = "sift"

const stateShapeDigits = 8

func (a Asker) record(state json.RawMessage, decision jev.Decision) error {
	if a.Ledger == nil {
		return nil
	}
	hash, err := ledger.Hash(state)
	if err != nil {
		return err
	}
	shape, err := stateShape(state)
	if err != nil {
		return err
	}
	answers, err := recordedAnswers(a.Set.QuestionsVersion, decision.Answers)
	if err != nil {
		return err
	}
	_, err = a.Ledger.Append(ledger.Row{
		Bench:        BenchName,
		Point:        Point,
		Questions:    a.Set.Name,
		Version:      a.Set.QuestionsVersion,
		Build:        decision.Build,
		Model:        decision.Alias,
		StateHash:    hash,
		StateBuilder: shape,
		State:        state,
		Answers:      answers,
		LatencyMS:    decision.Latency.Milliseconds(),
		Cost:         decision.Usage.Cost,
		RequestID:    decision.RequestID,
	})
	return err
}

func stateShape(state json.RawMessage) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(state, &fields); err != nil {
		return "", err
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	sum := sha256.Sum256([]byte(strings.Join(names, ",")))
	return Point + "." + hex.EncodeToString(sum[:])[:stateShapeDigits], nil
}

func recordedAnswers(wording int, answers map[string]jev.Answer) ([]ledger.Answer, error) {
	ids := make([]string, 0, len(answers))
	for id := range answers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	recorded := make([]ledger.Answer, 0, len(ids))
	for _, id := range ids {
		answer := answers[id]
		if answer.Kind != jev.QuestionNoul {
			return nil, fmt.Errorf("sift: %s answers %q with a %s, and this bench asks nouls only", Point, id, answer.Kind)
		}
		recorded = append(recorded, ledger.Answer{Question: id, Wording: wording, Kind: ledger.AnswerNoul, Noul: answer.Noul})
	}
	return recorded, nil
}
