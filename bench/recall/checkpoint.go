package recall

import (
	"strings"

	"tofu/bench/corpus"
	rc "tofu/internal/recall"
)

func CorpusSession(turn corpus.RecordedTurn) Session {
	steps := make([]SessionStep, len(turn.Steps))
	for i, step := range turn.Steps {
		calls := make([]SessionCall, len(step.ToolCalls))
		for j, call := range step.ToolCalls {
			calls[j] = SessionCall{Tool: call.Tool, Args: call.Args, RenderedBytes: int(call.ResultBytes)}
		}
		steps[i] = SessionStep{Index: step.Index, AssistantText: step.AssistantText, ToolCalls: calls}
	}
	return Session{ID: turn.ID, Task: turn.Task, Steps: steps}
}

func RawReplayText(turn corpus.RecordedTurn) string {
	var text strings.Builder
	for _, step := range turn.Steps {
		if step.AssistantText != "" {
			text.WriteString(step.AssistantText)
			text.WriteString("\n")
		}
		for _, call := range step.ToolCalls {
			text.WriteString(call.Tool)
			text.WriteString(" ")
			text.Write(call.Args)
			text.WriteString("\n")
		}
	}
	return text.String()
}

func FreshChildCarry(result ReplayResult) (rc.Carry, bool) {
	if len(result.Forks) == 0 {
		return rc.Carry{}, false
	}
	return result.Forks[len(result.Forks)-1].Carry, true
}

var checkpointStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "of": true, "to": true,
	"in": true, "on": true, "at": true, "is": true, "was": true, "did": true, "had": true,
	"any": true, "it": true, "its": true, "for": true, "with": true, "from": true, "by": true,
	"one": true, "each": true, "then": true, "say": true, "what": true, "three": true, "that": true,
}

func taskKeywords(task string) []string {
	var words []string
	seen := make(map[string]bool)
	for _, raw := range strings.Fields(task) {
		word := strings.ToLower(strings.Trim(raw, ".,?!:;\"'"))
		if len(word) < 4 || checkpointStopwords[word] || seen[word] {
			continue
		}
		seen[word] = true
		words = append(words, word)
	}
	return words
}

func KeywordOverlap(task, carry string) (matched, total int) {
	keywords := taskKeywords(task)
	lower := strings.ToLower(carry)
	for _, word := range keywords {
		if strings.Contains(lower, word) {
			matched++
		}
	}
	return matched, len(keywords)
}
