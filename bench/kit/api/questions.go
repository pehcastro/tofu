package api

import (
	"fmt"

	question "tofu/internal/judge/questions"
	"tofu/internal/models/classifiers/core"
	libraryquestions "tofu/library/questions"
)

func GateBattery() ([]core.Question, int, error) {
	layers, err := question.Layers(libraryquestions.Files(), "")
	if err != nil {
		return nil, 0, err
	}
	set, _, err := question.Resolve("tool_gate@1", layers)
	if err != nil {
		return nil, 0, err
	}
	return toJevBattery(set.Questions), set.QuestionsVersion, nil
}

func toJevBattery(questions []question.Question) []core.Question {
	out := make([]core.Question, 0, len(questions))
	for _, q := range questions {
		out = append(out, q.ToJev())
	}
	return out
}

var countSweepPool = []core.Question{
	nounQuestion("q01", "Does the state's cwd field start with /home?"),
	nounQuestion("q02", "Does the state name a bash tool call?"),
	nounQuestion("q03", "Does the state's context carry a user_recent_messages field?"),
	nounQuestion("q04", "Does the state's input field carry a command key?"),
	nounQuestion("q05", "Does the state's context carry flagged_untrusted_content?"),
	nounQuestion("q06", "Does the state name an agent called tofu?"),
	nounQuestion("q07", "Does the command in the state contain the word git?"),
	nounQuestion("q08", "Does the state's cwd field end with project?"),
	nounQuestion("q09", "Is the state a JSON object with more than two top level fields?"),
	nounQuestion("q10", "Does the state mention a file named notes.txt anywhere?"),
	nounQuestion("q11", "Does the state carry a reference_material field in its context?"),
	nounQuestion("q12", "Does the state's tool field equal bash?"),
}

func nounQuestion(id, instructions string) core.Question {
	return core.Question{
		ID:           id,
		Kind:         core.QuestionNoul,
		Instructions: instructions,
		True:         "yes, the state matches the question",
		False:        "no, the state does not match the question",
	}
}

func CountSweepQuestions(count int) []core.Question {
	if count > len(countSweepPool) {
		panic(fmt.Sprintf("bench: only %d count sweep questions are defined, asked for %d", len(countSweepPool), count))
	}
	return countSweepPool[:count]
}

const optionSweepFieldName = "target"

func OptionSweepQuestion(options int) (core.Question, string) {
	names := make([]string, options)
	for i := range names {
		names[i] = fmt.Sprintf("item_%d", i)
	}
	expected := names[options/2]
	opts := make([]core.Option, options)
	for i, name := range names {
		opts[i] = core.Option{Name: name}
	}
	q := core.Question{
		ID:           "pick",
		Kind:         core.QuestionChoice,
		Instructions: "The state has a field named target. Choose the option whose name equals it exactly.",
		Options:      opts,
	}
	return q, expected
}

func OptionSweepState(expected string) any {
	return map[string]any{optionSweepFieldName: expected}
}
