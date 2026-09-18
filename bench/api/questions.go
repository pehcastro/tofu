package api

import (
	"fmt"

	catalogquestions "boji/catalog/questions"
	"boji/internal/judge/jev"
	"boji/internal/judge/question"
)

func GateBattery() ([]jev.Question, int, error) {
	layers, err := question.DefaultLayers(catalogquestions.Files())
	if err != nil {
		return nil, 0, err
	}
	set, _, err := question.Resolve("tool_gate@1", layers)
	if err != nil {
		return nil, 0, err
	}
	return toJevBattery(set.Questions), set.QuestionsVersion, nil
}

func toJevBattery(questions []question.Question) []jev.Question {
	out := make([]jev.Question, 0, len(questions))
	for _, q := range questions {
		out = append(out, q.ToJev())
	}
	return out
}

var countSweepPool = []jev.Question{
	nounQuestion("q01", "Does the state's cwd field start with /home?"),
	nounQuestion("q02", "Does the state name a bash tool call?"),
	nounQuestion("q03", "Does the state's context carry a user_recent_messages field?"),
	nounQuestion("q04", "Does the state's input field carry a command key?"),
	nounQuestion("q05", "Does the state's context carry flagged_untrusted_content?"),
	nounQuestion("q06", "Does the state name an agent called boji?"),
	nounQuestion("q07", "Does the command in the state contain the word git?"),
	nounQuestion("q08", "Does the state's cwd field end with project?"),
	nounQuestion("q09", "Is the state a JSON object with more than two top level fields?"),
	nounQuestion("q10", "Does the state mention a file named notes.txt anywhere?"),
	nounQuestion("q11", "Does the state carry a reference_material field in its context?"),
	nounQuestion("q12", "Does the state's tool field equal bash?"),
}

func nounQuestion(id, instructions string) jev.Question {
	return jev.Question{
		ID:           id,
		Kind:         jev.QuestionNoul,
		Instructions: instructions,
		True:         "yes, the state matches the question",
		False:        "no, the state does not match the question",
	}
}

func CountSweepQuestions(count int) []jev.Question {
	if count > len(countSweepPool) {
		panic(fmt.Sprintf("bench: only %d count sweep questions are defined, asked for %d", len(countSweepPool), count))
	}
	return countSweepPool[:count]
}

const optionSweepFieldName = "target"

func OptionSweepQuestion(options int) (jev.Question, string) {
	names := make([]string, options)
	for i := range names {
		names[i] = fmt.Sprintf("item_%d", i)
	}
	expected := names[options/2]
	opts := make([]jev.Option, options)
	for i, name := range names {
		opts[i] = jev.Option{Name: name}
	}
	q := jev.Question{
		ID:           "pick",
		Kind:         jev.QuestionChoice,
		Instructions: "The state has a field named target. Choose the option whose name equals it exactly.",
		Options:      opts,
	}
	return q, expected
}

func OptionSweepState(expected string) any {
	return map[string]any{optionSweepFieldName: expected}
}
