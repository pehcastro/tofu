package shortlist

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"tofu/bench/corpus"
)

const sessionsDir = repoRoot + "/.tofu/sessions"

var questionsAShortlistFigureNeeds = NeededQuestions(4, 4, 1, 4)

type population struct {
	turns      int
	noTask     int
	noChange   int
	creation   int
	recordable int
	distinct   int
}

func countPopulation(turns []corpus.Turn) population {
	counted := population{turns: len(turns)}
	tasks := map[string]bool{}
	for _, turn := range turns {
		firstTouch := map[string]string{}
		changed := map[string]bool{}
		for _, step := range turn.Steps {
			for _, call := range step.ToolCalls {
				var args struct {
					Path string `json:"path"`
				}
				if json.Unmarshal(call.Args, &args) != nil || args.Path == "" {
					continue
				}
				path := strings.ReplaceAll(args.Path, `\`, "/")
				if _, seen := firstTouch[path]; !seen {
					firstTouch[path] = call.Tool
				}
				if call.Tool == "write" || call.Tool == "edit" {
					changed[path] = true
				}
			}
		}
		if strings.TrimSpace(turn.Task) == "" {
			counted.noTask++
			continue
		}
		if len(changed) == 0 {
			counted.noChange++
			continue
		}
		existed := false
		for path := range changed {
			if firstTouch[path] != "write" {
				existed = true
			}
		}
		if !existed {
			counted.creation++
			continue
		}
		counted.recordable++
		tasks[strings.TrimSpace(turn.Task)] = true
	}
	counted.distinct = len(tasks)
	return counted
}

func TestHowManyShortlistQuestionsTheRecordedSessionsCouldYield(t *testing.T) {
	if _, err := os.Stat(sessionsDir); err != nil {
		t.Skipf("no recorded sessions at %s on this machine, so the population cannot be counted here: %v", sessionsDir, err)
	}
	walked, err := corpus.WalkSessions(sessionsDir)
	if err != nil {
		t.Fatalf("WalkSessions: %v", err)
	}
	counted := countPopulation(walked.Turns)
	t.Logf("%d entries, %d turns read, %d unreadable: %d carry no task, %d change no file, %d only create files that did not exist, %d are recordable, %d of those carry a distinct task",
		walked.EntryCount, counted.turns, len(walked.Skipped), counted.noTask, counted.noChange, counted.creation, counted.recordable, counted.distinct)
	if counted.distinct >= questionsAShortlistFigureNeeds {
		t.Fatalf("%d distinct recordable turns is at or above the %d NeededQuestions(4,4,1,4) asks for: rebuild the corpus", counted.distinct, questionsAShortlistFigureNeeds)
	}
}

func TestCountPopulationSeparatesAnEditOfAnOldFileFromTheWritingOfANewOne(t *testing.T) {
	call := func(tool, path string) corpus.RecordedCall {
		return corpus.RecordedCall{Tool: tool, Args: json.RawMessage(`{"path":"` + path + `"}`)}
	}
	turn := func(task string, calls ...corpus.RecordedCall) corpus.Turn {
		return corpus.Turn{RecordedTurn: corpus.RecordedTurn{Task: task, Steps: []corpus.RecordedStep{{ToolCalls: calls}}}}
	}
	counted := countPopulation([]corpus.Turn{
		turn("", call("edit", "a.ts")),
		turn("read it", call("read", "a.ts")),
		turn("make a new file", call("write", "new.ts")),
		turn("change the old file", call("read", "a.ts"), call("write", "a.ts")),
		turn("change the old file", call("edit", "a.ts")),
	})
	want := population{turns: 5, noTask: 1, noChange: 1, creation: 1, recordable: 2, distinct: 1}
	if counted != want {
		t.Fatalf("countPopulation = %+v, want %+v", counted, want)
	}
}
