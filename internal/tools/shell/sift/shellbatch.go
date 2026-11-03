package sift

import (
	"fmt"
	"strings"

	"tofu/internal/judge/jev"
)

const ShellBatchPoint = ShellSchema + "@2"

type ShellChunk struct {
	Stream Stream `json:"stream"`
	Text   string `json:"text"`
}

type ShellRubric struct {
	Instructions string `json:"instructions"`
	True         string `json:"true"`
	False        string `json:"false"`
}

type ShellBatchState struct {
	Task     string       `json:"task"`
	Command  string       `json:"command"`
	ExitCode int          `json:"exit_code"`
	Chunks   []ShellChunk `json:"chunks"`
	Rubric   ShellRubric  `json:"rubric"`
}

type ShellBatch struct {
	Request jev.Request
	Indexes []int
}

type ShellBatchOverCeilingError struct {
	Command        string
	RequestBytes   int
	RequestTokens  int
	QuestionTokens int
	Caps           jev.WireCaps
}

func (e ShellBatchOverCeilingError) Error() string {
	return fmt.Sprintf("%q: one request is %d bytes, about %d tokens, and its largest question about %d, and %s accepts %d bytes, %d tokens and %d",
		e.Command, e.RequestBytes, e.RequestTokens, e.QuestionTokens, e.Caps.Name, e.Caps.MaxRequestBytes, e.Caps.MaxRequestTokens, e.Caps.MaxStateTokens)
}

func BuildShellBatch(rubric jev.Question, caps jev.WireCaps, model string, result Shell, units []Unit, task string) (ShellBatch, error) {
	state := ShellBatchState{
		Task:     task,
		Command:  result.Command,
		ExitCode: result.ExitCode,
		Rubric:   ShellRubric{Instructions: rubric.Instructions, True: rubric.True, False: rubric.False},
	}
	var questions []jev.Question
	var indexes []int
	for _, unit := range units {
		state.Chunks = append(state.Chunks, ShellChunk{Stream: unit.Stream, Text: strings.TrimRight(unit.Text, "\n")})
		if unit.Held != NotHeld {
			continue
		}
		chunk := fmt.Sprintf("chunks[%d]", unit.Index)
		questions = append(questions, jev.Question{
			ID:           fmt.Sprintf("%s_%d", NeededQuestion, unit.Index),
			Kind:         jev.QuestionNoul,
			Instructions: "Apply the rubric in the state to " + chunk + " and to no other chunk.",
			True:         "rubric.true holds for " + chunk + ".",
			False:        "rubric.false holds for " + chunk + ".",
		})
		indexes = append(indexes, unit.Index)
	}
	batch := ShellBatch{Request: jev.Request{State: state, Questions: questions}, Indexes: indexes}
	if len(indexes) == 0 {
		return batch, nil
	}
	whole, err := batch.Request.Encode(model)
	if err != nil {
		return ShellBatch{}, err
	}
	longest, err := jev.Request{State: state, Questions: questions[len(questions)-1:]}.Encode(model)
	if err != nil {
		return ShellBatch{}, err
	}
	over := ShellBatchOverCeilingError{Command: result.Command, RequestBytes: len(whole), RequestTokens: jev.EstimateTokens(len(whole)), QuestionTokens: jev.EstimateTokens(len(longest)), Caps: caps}
	if over.RequestTokens > caps.MaxRequestTokens || over.QuestionTokens > caps.MaxStateTokens || (caps.MaxRequestBytes > 0 && over.RequestBytes > caps.MaxRequestBytes) {
		return ShellBatch{}, over
	}
	return batch, nil
}

func (b ShellBatch) Scores(decision jev.Decision) map[int]float64 {
	scores := make(map[int]float64, len(b.Indexes))
	for i, index := range b.Indexes {
		if answer, answered := decision.Answers[b.Request.Questions[i].ID]; answered {
			scores[index] = answer.Noul
		}
	}
	return scores
}
