package startup

import (
	"strings"
	"testing"
)

const minRoundsForTest = 5

func measured(t *testing.T) Rounds {
	t.Helper()
	rounds, err := Measure(minRoundsForTest)
	if err != nil {
		t.Fatalf("measuring: %v", err)
	}
	return rounds
}

func TestShellResolutionHasAtLeastFiveSamplesWithAMedianAndAWorst(t *testing.T) {
	rounds := measured(t)
	if len(rounds.Shell.Samples) < minRoundsForTest {
		t.Fatalf("shell resolution has %d samples, want at least %d", len(rounds.Shell.Samples), minRoundsForTest)
	}
	if rounds.Shell.Worst < rounds.Shell.Median {
		t.Fatalf("worst %v is under median %v", rounds.Shell.Worst, rounds.Shell.Median)
	}
	t.Logf("shell resolution: median %v, worst %v over %d samples", rounds.Shell.Median, rounds.Shell.Worst, len(rounds.Shell.Samples))
}

func TestToolchainProbeIsMeasuredSeparatelyForGoAndNode(t *testing.T) {
	rounds := measured(t)
	if len(rounds.GoProbe.Samples) < minRoundsForTest || len(rounds.NodeProbe.Samples) < minRoundsForTest {
		t.Fatalf("want at least %d rounds each, got go=%d node=%d", minRoundsForTest, len(rounds.GoProbe.Samples), len(rounds.NodeProbe.Samples))
	}
	t.Logf("go probe: median %v worst %v; node probe: median %v worst %v",
		rounds.GoProbe.Median, rounds.GoProbe.Worst, rounds.NodeProbe.Median, rounds.NodeProbe.Worst)
}

func TestASecondTurnInTheSameDirectoryIsCheaperThanTheFirst(t *testing.T) {
	rounds := measured(t)
	if len(rounds.NodeWarm.Samples) < minRoundsForTest || len(rounds.NodeCold.Samples) < minRoundsForTest {
		t.Fatalf("want at least %d rounds each, got cold=%d warm=%d", minRoundsForTest, len(rounds.NodeCold.Samples), len(rounds.NodeWarm.Samples))
	}
	if rounds.NodeWarm.Median >= rounds.NodeCold.Median {
		t.Fatalf("the cached second turn (%v) was not cheaper than the first (%v)", rounds.NodeWarm.Median, rounds.NodeCold.Median)
	}
	t.Logf("node cold: median %v worst %v; node warm (cached): median %v worst %v",
		rounds.NodeCold.Median, rounds.NodeCold.Worst, rounds.NodeWarm.Median, rounds.NodeWarm.Worst)
}

func TestReportStatesTheThresholdBeforeTheHeadline(t *testing.T) {
	rounds := measured(t)
	rendered := Report{
		Date: "2026-01-01", Machine: "test", Rounds: minRoundsForTest,
		Shell: rounds.Shell, GoProbe: rounds.GoProbe, NodeProbe: rounds.NodeProbe,
		NodeCold: rounds.NodeCold, NodeWarm: rounds.NodeWarm,
	}.Render()
	thresholdAt := strings.Index(rendered, "Threshold")
	headlineAt := strings.Index(rendered, "Headline")
	if thresholdAt < 0 || headlineAt < 0 || thresholdAt > headlineAt {
		t.Fatalf("the threshold section must come before the headline: threshold at %d, headline at %d", thresholdAt, headlineAt)
	}
}
