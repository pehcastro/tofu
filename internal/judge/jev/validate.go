package jev

import (
	"strconv"

	"tofu/internal/konst"
	"tofu/internal/transport"
)

const floatSlack = 1e-9

func Validate(request Request, response Response) error {
	if len(response.Answers) != len(request.Questions) {
		return invalid("the response holds %d answers for %d questions", len(response.Answers), len(request.Questions))
	}
	for _, question := range request.Questions {
		answer, ok := response.Answers[question.ID]
		if !ok {
			return invalid("question %q was not answered", question.ID)
		}
		if answer.Kind != question.Kind {
			return invalid("question %q is a %s and was answered with a %s", question.ID, question.Kind, answer.Kind)
		}
		if err := validateAnswer(question, answer); err != nil {
			return err
		}
	}
	return nil
}

func validateAnswer(question Question, answer Answer) error {
	switch question.Kind {
	case QuestionNoul:
		if !inUnit(answer.Noul) {
			return invalid("noul %q is %g, outside zero to one", question.ID, answer.Noul)
		}
		return nil

	case QuestionChoice:
		options := make(map[string]bool, len(question.Options))
		for _, option := range question.Options {
			options[option.Name] = true
		}
		if !options[answer.Choice] {
			return invalid("choice %q chose %q, which is not in its criteria", question.ID, answer.Choice)
		}
		for name := range answer.Probabilities {
			if !options[name] {
				return invalid("choice %q reported a probability for %q, which is not in its criteria", question.ID, name)
			}
		}
		sum := 0.0
		chosen := answer.Probabilities[answer.Choice]
		leader, best := "", -1.0
		for _, option := range question.Options {
			probability := answer.Probabilities[option.Name]
			if !inUnit(probability) {
				return invalid("choice %q gave %q the probability %g, outside zero to one", question.ID, option.Name, probability)
			}
			if probability > best {
				leader, best = option.Name, probability
			}
			sum += probability
		}
		if best > chosen {
			return invalid("choice %q chose %q at %g while %q has %g", question.ID, answer.Choice, chosen, leader, best)
		}
		if !nearOne(sum) {
			return invalid("choice %q has probabilities summing to %g", question.ID, sum)
		}
		if !inUnit(answer.Confidence) {
			return invalid("choice %q reports confidence %g, outside zero to one", question.ID, answer.Confidence)
		}
		return nil

	case QuestionScore:
		levels := len(question.Levels)
		if len(answer.Probabilities) != levels {
			return invalid("score %q was asked for %d levels and answered with %d", question.ID, levels, len(answer.Probabilities))
		}
		sum := 0.0
		for level := 0; level < levels; level++ {
			probability, ok := answer.Probabilities[strconv.Itoa(level)]
			if !ok {
				return invalid("score %q has no probability for level %d", question.ID, level)
			}
			if !inUnit(probability) {
				return invalid("score %q gave level %d the probability %g, outside zero to one", question.ID, level, probability)
			}
			sum += probability
		}
		if !nearOne(sum) {
			return invalid("score %q has probabilities summing to %g", question.ID, sum)
		}
		if answer.Score < 0 || answer.Score > float64(levels-1) {
			return invalid("score %q is %g, outside zero to %d", question.ID, answer.Score, levels-1)
		}
		if !inUnit(answer.Confidence) {
			return invalid("score %q reports confidence %g, outside zero to one", question.ID, answer.Confidence)
		}
		return nil
	}
	panic("jev: unknown question kind")
}

func inUnit(value float64) bool {
	return value >= 0 && value <= 1
}

func nearOne(sum float64) bool {
	return sum >= 1-konst.JudgeSumTolerance-floatSlack && sum <= 1+konst.JudgeSumTolerance+floatSlack
}

func invalid(format string, args ...any) error {
	return transport.Fail("jev.Validate", transport.KindInvalidAnswer, nil, format, args...)
}
