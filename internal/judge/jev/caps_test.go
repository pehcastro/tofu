package jev

import "testing"

func TestEstimateTokensMatchesTheMeasuredCurveExactly(t *testing.T) {
	for i, bytes := range openRouterCurveBytes {
		billed := openRouterCurveTokens[i]
		if got := EstimateTokens(bytes); got != billed {
			t.Fatalf("EstimateTokens(%d) = %d, want exactly %d, the billed figure BOJI-006 measured there", bytes, got, billed)
		}
	}
}

func TestEstimateTokensBelowTheSmallestMeasuredPointIsFlatAndCoversTheWorstLiveError(t *testing.T) {
	const liveBytes, liveBilled = 804, 503
	got := EstimateTokens(liveBytes)
	if got != openRouterCurveTokens[0] {
		t.Fatalf("EstimateTokens(%d) = %d, want the flat floor of %d, the smallest measured point", liveBytes, got, openRouterCurveTokens[0])
	}
	if got < liveBilled {
		t.Fatalf("EstimateTokens(%d) = %d, want at least the %d tokens the live call billed", liveBytes, got, liveBilled)
	}
}

func TestEstimateBytesBelowTheSmallestMeasuredPointGrantsNothing(t *testing.T) {
	if got := EstimateBytes(openRouterCurveTokens[0]); got != 0 {
		t.Fatalf("EstimateBytes(%d) = %d, want 0, there is no data below the smallest measured point to justify a byte count", openRouterCurveTokens[0], got)
	}
}

func TestEstimateTokensInterpolatesBetweenMeasuredPoints(t *testing.T) {
	got := EstimateTokens(30000)
	if got <= 6748 || got >= 19446 {
		t.Fatalf("EstimateTokens(30000) = %d, want a value between the 4k and 12k fixtures", got)
	}
}

func TestEstimateTokensExtrapolatesBeyondTheLastMeasuredPointFasterThanTheLastSegment(t *testing.T) {
	last := len(openRouterCurveBytes) - 1
	extra := 10000
	got := EstimateTokens(openRouterCurveBytes[last] + extra)
	lastSegmentRate := openRouterCurveTokens[last] + extra*(openRouterCurveTokens[last]-openRouterCurveTokens[last-1])/(openRouterCurveBytes[last]-openRouterCurveBytes[last-1])
	if got <= lastSegmentRate {
		t.Fatalf("EstimateTokens(%d) = %d, want more than %d, what continuing the flattening trend of the last segment would give", openRouterCurveBytes[last]+extra, got, lastSegmentRate)
	}
}

func TestEstimateBytesRoundTripsWithoutExceedingTheCeiling(t *testing.T) {
	for _, ceiling := range []int{32000, 64000, 920, 32086} {
		bytes := EstimateBytes(ceiling)
		if got := EstimateTokens(bytes); got > ceiling {
			t.Fatalf("EstimateBytes(%d) = %d bytes, but EstimateTokens of that is %d, over the ceiling it was meant to respect", ceiling, bytes, got)
		}
	}
}

func TestEstimateBytesOfTheStateCeilingAgainstBoji006sLargestFixture(t *testing.T) {
	const fixtureBytes, fixtureBilledTokens = 90411, 32086
	got := EstimateBytes(32000)
	if got != 90165 {
		t.Fatalf("EstimateBytes(32000) = %d, want 90165: the exact interpolation of BOJI-006's own two largest fixtures with no invented margin", got)
	}
	if got >= fixtureBytes {
		t.Fatalf("EstimateBytes(32000) = %d, at or past the 28k fixture's %d bytes: the fixture would now pass, which means its %d billed tokens no longer exceed 32000 and this test needs rewriting, not just a number change", got, fixtureBytes, fixtureBilledTokens)
	}
}

func TestEstimateTokensAndBytesOfZeroOrLess(t *testing.T) {
	if EstimateTokens(0) != 0 || EstimateTokens(-1) != 0 {
		t.Fatal("expected zero for zero or negative bytes")
	}
	if EstimateBytes(0) != 0 || EstimateBytes(-1) != 0 {
		t.Fatal("expected zero for zero or negative tokens")
	}
}
