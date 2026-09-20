package turn

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"tofu/internal/sys"
	"tofu/internal/turn/tools"
)

type RecordedCall struct {
	Tool  string          `json:"tool"`
	Args  json.RawMessage `json:"args,omitempty"`
	Error string          `json:"error,omitempty"`
}

type RecordedStep struct {
	Index     int            `json:"index"`
	ToolCalls []RecordedCall `json:"tool_calls,omitempty"`
}

type RecordedTurn struct {
	ID          string         `json:"id"`
	Steps       []RecordedStep `json:"steps"`
	WallClockMS int64          `json:"wall_clock_ms"`
}

func ReadRecordedTurn(path string) (RecordedTurn, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		return RecordedTurn{}, err
	}
	var recorded RecordedTurn
	if err := json.Unmarshal(data, &recorded); err != nil {
		return RecordedTurn{}, fmt.Errorf("bench: %s is not a recorded turn: %w", path, err)
	}
	if len(recorded.Steps) == 0 || recorded.WallClockMS == 0 {
		return RecordedTurn{}, fmt.Errorf("bench: %s carries no step and no wall clock", path)
	}
	return recorded, nil
}

const (
	ArmRecorded = "as recorded"
	ArmLayered  = "with the call memo and the repair rule"
)

type ReplayArm struct {
	Name        string
	Steps       int
	Calls       int
	CacheHits   int
	Retries     int
	Repaired    int
	Undecided   int
	WallClockMS float64
}

var repairableFailure = regexp.MustCompile(`was asked for occurrence \d+ and the file holds 1`)

func recordedPath(call RecordedCall) string {
	var args struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(call.Args, &args)
	return args.Path
}

func Replay(recorded RecordedTurn) []ReplayArm {
	asRecorded := ReplayArm{Name: ArmRecorded, Steps: len(recorded.Steps), WallClockMS: float64(recorded.WallClockMS)}
	layered := ReplayArm{Name: ArmLayered}
	perStep := float64(recorded.WallClockMS) / float64(len(recorded.Steps))

	seen := map[string]bool{}
	repairedBefore := ""
	for _, step := range recorded.Steps {
		kept := 0
		for _, call := range step.ToolCalls {
			asRecorded.Calls++
			target := call.Tool + " " + recordedPath(call)
			retryOf := repairedBefore
			repairedBefore = ""
			if retryOf == target {
				layered.Retries++
				continue
			}
			if !tools.SideEffectFree(call.Tool) {
				clear(seen)
			} else if key, keyed := tools.CallKey(call.Tool, call.Args); keyed {
				if seen[key] {
					layered.CacheHits++
					continue
				}
				seen[key] = true
			}
			kept++
			layered.Calls++
			switch {
			case repairableFailure.MatchString(call.Error):
				layered.Repaired++
				repairedBefore = target
			case call.Error != "":
				layered.Undecided++
			}
		}
		if kept > 0 || len(step.ToolCalls) == 0 {
			layered.Steps++
		}
	}
	layered.WallClockMS = float64(recorded.WallClockMS) - float64(asRecorded.Steps-layered.Steps)*perStep
	return []ReplayArm{asRecorded, layered}
}

func RenderReplay(recorded RecordedTurn, arms []ReplayArm) string {
	var out strings.Builder
	fmt.Fprintf(&out, "recorded turn %s: %d steps, %d wall clock ms, %.0f ms per step\n",
		recorded.ID, len(recorded.Steps), recorded.WallClockMS, float64(recorded.WallClockMS)/float64(len(recorded.Steps)))
	for _, arm := range arms {
		fmt.Fprintf(&out, "arm %q: steps %d, calls %d, cache hits %d, retries removed %d, repaired %d, failures the record cannot decide %d, wall clock %.0f ms\n",
			arm.Name, arm.Steps, arm.Calls, arm.CacheHits, arm.Retries, arm.Repaired, arm.Undecided, arm.WallClockMS)
	}
	out.WriteString("the wall clock of the second arm is derived: the recorded wall clock less the removed steps at the recorded mean cost of a step, never a re-measurement, because the recording holds no model response to replay\n")
	return out.String()
}
