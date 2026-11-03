package recall

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	rc "tofu/internal/agent/context/recall"
	"tofu/internal/agent/loop"
	"tofu/internal/konst"
	"tofu/internal/store/session"
)

const (
	fillWatermarkPercent = 40
	sessionsExpected     = 60
)

type recordedFill struct {
	ID           string
	Model        string
	Steps        int
	PeakRequest  int
	PeakStep     int
	PeakEstimate int
	Window       int
	Compactions  int
	Forks        int
}

func fillOf(t *testing.T, header session.Header, events []session.Event) recordedFill {
	t.Helper()
	fill := recordedFill{ID: header.ID, Model: header.Model}
	if header.Model == replayedModel {
		fill.Window = replayedModelWindow
	}
	for _, event := range events {
		if event.Kind != session.EventStep {
			continue
		}
		var step turn.StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			t.Fatalf("a step of %s does not parse: %v", header.ID, err)
		}
		fill.Steps++
		if step.Compaction != nil {
			fill.Compactions++
		}
		if step.Fork != nil {
			fill.Forks++
		}
		request := step.PromptTokens + step.CacheReadTokens + step.CacheWriteTokens
		if request <= fill.PeakRequest {
			continue
		}
		fill.PeakRequest, fill.PeakStep = request, step.Index
		if step.Occupancy != nil {
			fill.PeakEstimate = step.Occupancy.Total()
		}
	}
	return fill
}

func recordedFills(t *testing.T, recordedBy time.Time) ([]recordedFill, []string, int) {
	t.Helper()
	store := session.NewStore(realSessionsDir(t))
	listing, err := store.Listing()
	if err != nil {
		t.Fatalf("read the recorded sessions: %v", err)
	}
	var billed []recordedFill
	var unbilled []string
	for _, skip := range listing.Skipped {
		unbilled = append(unbilled, skip.ID+": "+skip.Reason.Error())
	}
	later := 0
	for _, header := range listing.Sessions {
		if header.At.After(recordedBy) {
			later++
			continue
		}
		events, err := store.Body(header.ID)
		if err != nil {
			unbilled = append(unbilled, header.ID+": no readable body")
			continue
		}
		fill := fillOf(t, header, events)
		if fill.PeakRequest == 0 {
			unbilled = append(unbilled, fmt.Sprintf("%s: %d steps, no step reports a billed request", header.ID, fill.Steps))
			continue
		}
		billed = append(billed, fill)
	}
	sort.Slice(billed, func(i, j int) bool { return billed[i].PeakRequest > billed[j].PeakRequest })
	return billed, unbilled, later
}

func TestHowFullTheRecordedTurnsEverGotAgainstTheOperatingCeilingAndAgainstTheModelWindow(t *testing.T) {
	billed, unbilled, later := recordedFills(t, reachReportGeneratedAt)
	t.Logf("%d sessions recorded after %s, not read", later, reachReportGeneratedAt.Format(time.RFC3339))
	if len(billed)+len(unbilled) < sessionsExpected {
		t.Fatalf("%d sessions read under %s and %d are on disk: the measurement is over a corpus that is not there",
			len(billed)+len(unbilled), realSessionsDir(t), sessionsExpected)
	}
	ceiling := konst.ContextCeilingTokens
	target := rc.ShippedBands().Target()

	var table strings.Builder
	fmt.Fprintf(&table, "operating ceiling %d tokens, compaction target %d tokens\n", ceiling, target)
	fmt.Fprintf(&table, "%-26s %5s %14s %5s %9s %9s %9s %10s %6s\n",
		"session", "steps", "peak request", "step", "of ceiling", "of target", "of window", "window", "events")
	steps, buckets := 0, map[int]int{}
	for _, fill := range billed {
		steps += fill.Steps
		buckets[rc.FillPercent(fill.PeakRequest, ceiling)/10]++
		fmt.Fprintf(&table, "%-26s %5d %14d %5d %8d%% %8d%% %8d%% %10d %6d\n",
			fill.ID, fill.Steps, fill.PeakRequest, fill.PeakStep,
			rc.FillPercent(fill.PeakRequest, ceiling), rc.FillPercent(fill.PeakRequest, target),
			rc.FillPercent(fill.PeakRequest, fill.Window), fill.Window, fill.Compactions+fill.Forks)
	}
	t.Logf("\n%s", table.String())

	var spread strings.Builder
	spread.WriteString("peak request as a share of the operating ceiling\n")
	for decile := 0; decile <= 10; decile++ {
		if buckets[decile] > 0 {
			fmt.Fprintf(&spread, "%3d to %3d%%  %3d sessions\n", decile*10, decile*10+9, buckets[decile])
		}
	}
	t.Logf("\n%s", spread.String())
	t.Logf("%d sessions carry a billed request over %d steps; %d carry none:\n    %s",
		len(billed), steps, len(unbilled), strings.Join(unbilled, "\n    "))

	largest := billed[0]
	t.Logf("the largest request ever billed here is %d tokens, %s step %d on %s: %d%% of the %d token operating ceiling, %d%% of the %d token target, %d%% of the model's own %d token window, and the estimator read that step at %d tokens",
		largest.PeakRequest, largest.ID, largest.PeakStep, largest.Model,
		rc.FillPercent(largest.PeakRequest, ceiling), ceiling,
		rc.FillPercent(largest.PeakRequest, target), target,
		rc.FillPercent(largest.PeakRequest, largest.Window), largest.Window, largest.PeakEstimate)

	if worst := rc.FillPercent(largest.PeakRequest, ceiling); worst >= fillWatermarkPercent {
		t.Fatalf("a recorded turn reached %d%% of the %d token operating ceiling at %s step %d, %d tokens: the ceiling is now the binding constraint and the finding this test carries is stale",
			worst, ceiling, largest.ID, largest.PeakStep, largest.PeakRequest)
	}
}
