package schemas

import (
	"sort"

	"tofu/bench/corpus"
)

type StepUsage struct {
	Index            int
	PromptTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	CompletionTokens int
	ToolCalls        []string
}

type TurnUsage struct {
	ID    string
	Steps []StepUsage
}

func (t TurnUsage) FirstCacheWrite() (int, bool) {
	for _, step := range t.Steps {
		if step.CacheWriteTokens > 0 {
			return step.CacheWriteTokens, true
		}
	}
	return 0, false
}

func (t TurnUsage) SumFreshInput() int { return t.sum(func(s StepUsage) int { return s.PromptTokens }) }

func (t TurnUsage) SumCacheRead() int {
	return t.sum(func(s StepUsage) int { return s.CacheReadTokens })
}

func (t TurnUsage) SumCacheWrite() int {
	return t.sum(func(s StepUsage) int { return s.CacheWriteTokens })
}

func (t TurnUsage) sum(field func(StepUsage) int) int {
	total := 0
	for _, step := range t.Steps {
		total += field(step)
	}
	return total
}

func (t TurnUsage) CalledToolNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, step := range t.Steps {
		for _, name := range step.ToolCalls {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func ReadSessions(dir string) (usable []TurnUsage, skipped []corpus.SkippedTurn, err error) {
	walked, err := corpus.WalkSessions(dir)
	if err != nil {
		return nil, nil, err
	}
	skipped = append(skipped, walked.Skipped...)
	for _, turn := range walked.Turns {
		usage, reason := toTurnUsage(turn.RecordedTurn)
		if reason != "" {
			skipped = append(skipped, corpus.SkippedTurn{Path: turn.ID, Reason: reason})
			continue
		}
		usable = append(usable, usage)
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].ID < usable[j].ID })
	return usable, skipped, nil
}

func toTurnUsage(recorded corpus.RecordedTurn) (TurnUsage, string) {
	if len(recorded.Steps) == 0 {
		return TurnUsage{}, "carries no steps"
	}
	carriesCache := false
	turn := TurnUsage{ID: recorded.ID}
	for _, step := range recorded.Steps {
		names := make([]string, len(step.ToolCalls))
		for i, call := range step.ToolCalls {
			names[i] = call.Tool
		}
		var cacheRead, cacheWrite int
		if step.CacheReadTokens != nil {
			cacheRead = *step.CacheReadTokens
			carriesCache = true
		}
		if step.CacheWriteTokens != nil {
			cacheWrite = *step.CacheWriteTokens
			carriesCache = true
		}
		turn.Steps = append(turn.Steps, StepUsage{
			Index:            step.Index,
			PromptTokens:     step.PromptTokens,
			CacheReadTokens:  cacheRead,
			CacheWriteTokens: cacheWrite,
			CompletionTokens: step.CompletionTokens,
			ToolCalls:        names,
		})
	}
	if !carriesCache {
		return TurnUsage{}, "carries no cache token fields, an older recording predating cache accounting"
	}
	return turn, ""
}
