package wording

import (
	"boji/internal/judge/jev"
	"boji/internal/judge/question"
)

const (
	setV1Path = "catalog/questions/tool_gate@1.yaml"
	setV2Path = "catalog/questions/tool_gate@2.yaml"
)

func loadBattery(path string) ([]jev.Question, error) {
	set, err := question.Load(path)
	if err != nil {
		return nil, err
	}
	questions := make([]jev.Question, 0, len(set.Questions))
	for _, q := range set.Questions {
		questions = append(questions, q.ToJev())
	}
	return questions, nil
}
