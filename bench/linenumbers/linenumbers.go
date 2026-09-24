package linenumbers

import (
	"tofu/bench/corpus"
	"tofu/internal/konst"
)

type Result struct {
	SessionsDir     string
	Turns           int
	Skipped         []corpus.SkippedTurn
	ReadCalls       int
	ReadBytes       int64
	ReadEstTokens   int64
	CacheReadSteps  int
	CacheReadTokens int64
}

func Run(sessionsDir string) (Result, error) {
	walked, err := corpus.WalkSessions(sessionsDir)
	if err != nil {
		return Result{}, err
	}
	result := Result{SessionsDir: sessionsDir, Turns: len(walked.Turns), Skipped: walked.Skipped}
	for _, turn := range walked.Turns {
		for _, step := range turn.Steps {
			if step.CacheReadTokens != nil {
				result.CacheReadSteps++
				result.CacheReadTokens += int64(*step.CacheReadTokens)
			}
			for _, call := range step.ToolCalls {
				if call.Tool != "read" {
					continue
				}
				result.ReadCalls++
				result.ReadBytes += call.RenderedBytes
			}
		}
	}
	result.ReadEstTokens = result.ReadBytes / konst.SearchBytesPerToken
	return result, nil
}
