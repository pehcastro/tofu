package recall

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"tofu/internal/konst"
	rc "tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
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

func recordedFills(t *testing.T) ([]recordedFill, []string) {
	t.Helper()
	store := session.NewStore(sys.RecordedStateDir("sessions"))
	listing, err := store.Listing()
	if err != nil {
		t.Fatalf("read the recorded sessions: %v", err)
	}
	var billed []recordedFill
	var unbilled []string
	for _, skip := range listing.Skipped {
		unbilled = append(unbilled, skip.ID+": "+skip.Reason.Error())
	}
	for _, header := range listing.Sessions {
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
	return billed, unbilled
}

func TestHowFullTheRecordedTurnsEverGotAgainstTheOperatingCeilingAndAgainstTheModelWindow(t *testing.T) {
	billed, unbilled := recordedFills(t)
	if len(billed)+len(unbilled) < sessionsExpected {
		t.Fatalf("%d sessions read under %s and %d are on disk: the measurement is over a corpus that is not there",
			len(billed)+len(unbilled), sys.RecordedStateDir("sessions"), sessionsExpected)
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

func TestWhatCompactionWouldCostOnTheLargestRecordedTurnAtEachCeiling(t *testing.T) {
	cfg, recorded := recordedTurn(t)
	peak := replay(t, cfg, rc.ShippedBands(), recorded, armNothing).Peak.Total()

	watermarked := peak * 100 / fillWatermarkPercent
	ceilings := []int{konst.ContextCeilingTokens, 180000, 150000, watermarked, 120000, 100000, 60000, 40000, 20000}
	sort.Slice(ceilings, func(i, j int) bool { return ceilings[i] > ceilings[j] })
	billedPeak := 0
	for _, step := range recorded.Steps {
		billedPeak = max(billedPeak, step.PromptTokens+step.CacheReadTokens)
	}

	var table strings.Builder
	fmt.Fprintf(&table, "turn %s, %d steps, peaking at %d tokens with nothing done, %d tokens billed\n",
		recorded.ID, len(recorded.Steps), peak, billedPeak)
	fmt.Fprintf(&table, "%9s %9s %9s %12s %14s %7s %9s %8s\n",
		"ceiling", "target", "of ceiling", "compactions", "tokens dropped", "forks", "refetches", "blind")
	firstBinding, shippedDrops := 0, 0
	for _, ceiling := range ceilings {
		bands := rc.BandsOf(ceiling)
		rewrite := replay(t, cfg, bands, recorded, armRewrite)
		forked := replay(t, cfg, bands, recorded, armHandles)
		compactions, dropped := 0, 0
		for _, step := range rewrite.Steps {
			if len(step.Drops) > 0 {
				compactions++
			}
		}
		for _, drop := range rewrite.Drops {
			dropped += drop.TokensFreed
		}
		if firstBinding == 0 && compactions > 0 {
			firstBinding = ceiling
		}
		if ceiling == konst.ContextCeilingTokens {
			shippedDrops = len(rewrite.Drops)
		}
		fmt.Fprintf(&table, "%9d %9d %9d%% %12d %14d %7d %9d %8d\n",
			ceiling, bands.Target(), rc.FillPercent(peak, ceiling), compactions, dropped,
			len(forked.Forks), len(forked.Refetches), forked.BlindRefetches())
	}
	t.Logf("\n%s", table.String())

	if shippedDrops != 0 {
		t.Fatalf("the largest recorded turn dropped %d results at the shipped %d token ceiling, so compaction is already firing on real traffic",
			shippedDrops, konst.ContextCeilingTokens)
	}
	if firstBinding == 0 {
		t.Fatal("no ceiling in the sweep makes the largest recorded turn compact, so the sweep says nothing about what compaction costs")
	}
	t.Logf("the largest recorded turn first compacts at a %d token ceiling, a %d token target, where it peaks at %d%% of the ceiling",
		firstBinding, rc.BandsOf(firstBinding).Target(), rc.FillPercent(peak, firstBinding))
	t.Logf("a %d token ceiling is the widest that holds this turn's %d token peak under %d%% fill",
		watermarked, peak, fillWatermarkPercent)
}
