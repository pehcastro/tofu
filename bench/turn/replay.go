package turn

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"tofu/bench/corpus"
	"tofu/internal/turn/tools"
)

type (
	RecordedCall = corpus.RecordedCall
	RecordedStep = corpus.RecordedStep
	RecordedTurn = corpus.RecordedTurn
)

func ReadRecordedTurn(path string) (RecordedTurn, error) {
	return corpus.ReadTurn(path)
}

const (
	ArmRecorded = "as recorded"
	ArmLayered  = "with the call memo and the repair rule"
)

type ReplayArm struct {
	Name           string
	Steps          int
	Calls          int
	CacheHits      int
	Retries        int
	Repaired       int
	Refusals       int
	Undecided      int
	BytesReturned  int64
	AnswersChanged int
	WallClockMS    float64
}

var repairableFailure = regexp.MustCompile(`was asked for occurrence \d+ and the file holds 1`)

var refusedFailure = regexp.MustCompile(`nothing was changed|matches no line in the file|matches \d+ lines? \([^)]*\) and an edit must name exactly one`)

func recordedPath(call RecordedCall) string {
	var args struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(call.Args, &args)
	return args.Path
}

type BytesSkipped struct {
	ByCache int64
	ByRetry int64
}

func Replay(recorded RecordedTurn) ([]ReplayArm, BytesSkipped) {
	asRecorded := ReplayArm{Name: ArmRecorded, Steps: len(recorded.Steps), WallClockMS: float64(recorded.WallClockMS)}
	layered := ReplayArm{Name: ArmLayered}
	perStep := float64(recorded.WallClockMS) / float64(len(recorded.Steps))
	var skipped BytesSkipped

	seen := map[string]bool{}
	repairedBefore := ""
	for _, step := range recorded.Steps {
		kept := 0
		for _, call := range step.ToolCalls {
			asRecorded.Calls++
			asRecorded.BytesReturned += call.ResultBytes
			if refusedFailure.MatchString(call.Error) {
				asRecorded.Refusals++
			}

			target := call.Tool + " " + recordedPath(call)
			retryOf := repairedBefore
			repairedBefore = ""
			if retryOf == target {
				layered.Retries++
				skipped.ByRetry += call.ResultBytes
				continue
			}
			if !tools.SideEffectFree(call.Tool) {
				clear(seen)
			} else if key, keyed := tools.CallKey(call.Tool, call.Args); keyed {
				if seen[key] {
					layered.CacheHits++
					layered.AnswersChanged++
					skipped.ByCache += call.ResultBytes
					continue
				}
				seen[key] = true
			}
			kept++
			layered.Calls++
			layered.BytesReturned += call.ResultBytes
			switch {
			case repairableFailure.MatchString(call.Error):
				layered.Repaired++
				layered.AnswersChanged++
				repairedBefore = target
			case refusedFailure.MatchString(call.Error):
				layered.Refusals++
			case call.Error != "":
				layered.Undecided++
			}
		}
		if kept > 0 || len(step.ToolCalls) == 0 {
			layered.Steps++
		}
	}
	layered.WallClockMS = float64(recorded.WallClockMS) - float64(asRecorded.Steps-layered.Steps)*perStep
	return []ReplayArm{asRecorded, layered}, skipped
}

func RenderReplay(recorded RecordedTurn, arms []ReplayArm) string {
	var out strings.Builder
	fmt.Fprintf(&out, "recorded turn %s: %d steps, %d wall clock ms, %.0f ms per step\n",
		recorded.ID, len(recorded.Steps), recorded.WallClockMS, float64(recorded.WallClockMS)/float64(len(recorded.Steps)))
	for _, arm := range arms {
		fmt.Fprintf(&out, "arm %q: steps %d, calls %d, cache hits %d, retries removed %d, repaired %d, refused %d, failures the record cannot decide %d, bytes returned %d, answers changed %d, wall clock %.0f ms\n",
			arm.Name, arm.Steps, arm.Calls, arm.CacheHits, arm.Retries, arm.Repaired, arm.Refusals, arm.Undecided, arm.BytesReturned, arm.AnswersChanged, arm.WallClockMS)
	}
	out.WriteString("the wall clock of the second arm is derived: the recorded wall clock less the removed steps at the recorded mean cost of a step, never a re-measurement, because the recording holds no model response to replay\n")
	return out.String()
}
