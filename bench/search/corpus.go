package search

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"tofu/bench/corpus"
)

type Recorded struct {
	Turn      string
	Pattern   string
	Path      string
	MaxTokens int
}

type Corpus struct {
	Dir     string
	Turns   int
	Later   int
	Rows    []Recorded
	Skipped []string
}

type recordedArgs struct {
	Pattern   string `json:"pattern"`
	Path      string `json:"path"`
	MaxTokens int    `json:"max_tokens"`
}

func Load(dir string, recordedBy time.Time) (Corpus, error) {
	live, err := corpus.WalkSessions(dir)
	if err != nil {
		return Corpus{}, err
	}
	walked := live.RecordedBy(recordedBy)
	loaded := Corpus{Dir: dir, Turns: len(walked.Turns), Later: walked.Later}
	for _, skipped := range walked.Skipped {
		loaded.Skipped = append(loaded.Skipped, skipped.Path+": "+skipped.Reason)
	}
	for _, turn := range walked.Turns {
		for _, step := range turn.Steps {
			for _, call := range step.ToolCalls {
				if call.Tool != "search" {
					continue
				}
				var args recordedArgs
				if err := json.Unmarshal(call.Args, &args); err != nil {
					return Corpus{}, fmt.Errorf("bench/search: %s step %d: %w", turn.ID, step.Index, err)
				}
				if leaks := corpus.LeaksIn(string(call.Args)); len(leaks) > 0 {
					return Corpus{}, fmt.Errorf("bench/search: %s step %d still carries %q after the scrub", turn.ID, step.Index, leaks)
				}
				if strings.TrimSpace(args.Pattern) == "" {
					loaded.Skipped = append(loaded.Skipped, turn.ID+": a recorded search carries no pattern")
					continue
				}
				loaded.Rows = append(loaded.Rows, Recorded{Turn: turn.ID, Pattern: args.Pattern, Path: args.Path, MaxTokens: args.MaxTokens})
			}
		}
	}
	slices.SortStableFunc(loaded.Rows, func(left, right Recorded) int {
		return strings.Compare(left.Turn+left.Pattern, right.Turn+right.Pattern)
	})
	return loaded, nil
}
