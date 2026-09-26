package turn

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"tofu/bench/corpus"
	"tofu/internal/turn/tools"
)

type Corpus struct {
	Dir              string
	EntryCount       int
	Turns            []RecordedTurn
	Skipped          []corpus.SkippedTurn
	JSONLDirs        []string
	JSONLDirsSeen    int
	JSONLDirsSkipped []string
}

func ReadCorpus(dir string) (Corpus, error) {
	walked, err := corpus.WalkSessions(dir)
	if err != nil {
		return Corpus{}, err
	}
	turnCorpus := Corpus{Dir: dir, EntryCount: walked.EntryCount, Skipped: append([]corpus.SkippedTurn{}, walked.Skipped...)}
	for _, entry := range walked.Turns {
		isDir := entry.Schema != corpus.SchemaSingleFile
		if isDir {
			turnCorpus.JSONLDirsSeen++
		}
		if !entry.WallClockRecorded {
			turnCorpus.Skipped = append(turnCorpus.Skipped, corpus.SkippedTurn{Path: entry.ID, Reason: corpus.ErrNoWallClock.Error()})
			if isDir {
				turnCorpus.JSONLDirsSkipped = append(turnCorpus.JSONLDirsSkipped, entry.ID)
			}
			continue
		}
		turnCorpus.Turns = append(turnCorpus.Turns, entry.RecordedTurn)
		if isDir {
			turnCorpus.JSONLDirs = append(turnCorpus.JSONLDirs, entry.ID)
		}
	}
	return turnCorpus, nil
}

const SameQuestionMethod = "same tool, CallKey differs, and a reader's subject matches: for read, artifact_fetch and fetch the subject is the cleaned path or handle; for grep, search, symbols and glob it is the pattern lowercased with runs of whitespace collapsed to one space. pairs are counted within one turn only, because the memo's scope is one turn."

func sameQuestionSubject(toolName string, raw json.RawMessage) (string, bool) {
	var args map[string]any
	if len(raw) == 0 {
		return "", false
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", false
	}
	switch toolName {
	case "read", "artifact_fetch", "fetch":
		if v, ok := args["path"].(string); ok {
			return path.Clean(filepath.ToSlash(v)), true
		}
		if v, ok := args["handle"].(string); ok {
			return v, true
		}
	case "grep", "search", "symbols", "glob":
		if v, ok := args["pattern"].(string); ok {
			return strings.ToLower(strings.Join(strings.Fields(v), " ")), true
		}
	}
	return "", false
}

func SameQuestionDifferentKeyPairs(turns []RecordedTurn) (total int, byTool map[string]int) {
	byTool = map[string]int{}
	for _, recorded := range turns {
		var calls []RecordedCall
		for _, step := range recorded.Steps {
			for _, call := range step.ToolCalls {
				if tools.SideEffectFree(call.Tool) {
					calls = append(calls, call)
				}
			}
		}
		for i := 0; i < len(calls); i++ {
			for j := i + 1; j < len(calls); j++ {
				if calls[i].Tool != calls[j].Tool {
					continue
				}
				keyI, okI := tools.CallKey(calls[i].Tool, calls[i].Args)
				keyJ, okJ := tools.CallKey(calls[j].Tool, calls[j].Args)
				if !okI || !okJ || keyI == keyJ {
					continue
				}
				subjectI, hasI := sameQuestionSubject(calls[i].Tool, calls[i].Args)
				subjectJ, hasJ := sameQuestionSubject(calls[j].Tool, calls[j].Args)
				if hasI && hasJ && subjectI == subjectJ {
					total++
					byTool[calls[i].Tool]++
				}
			}
		}
	}
	return total, byTool
}

func (c Corpus) String() string {
	return fmt.Sprintf("%s: %d entries, %d read, %d skipped", c.Dir, c.EntryCount, len(c.Turns), len(c.Skipped))
}
