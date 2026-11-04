package wording

import (
	question "tofu/internal/judge/questions"
	"tofu/internal/models/classifiers/core"
)

const (
	setV1Path = "library/questions/tool_gate@1.yaml"
	setV2Path = "library/questions/tool_gate@2.yaml"
)

func loadBattery(path string) ([]core.Question, error) {
	set, err := question.Load(path)
	if err != nil {
		return nil, err
	}
	questions := make([]core.Question, 0, len(set.Questions))
	for _, q := range set.Questions {
		questions = append(questions, q.ToJev())
	}
	return questions, nil
}
