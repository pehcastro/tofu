package cost

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	benchapi "boji/bench/api"
	"boji/bench/report"
	"boji/internal/judge/jev"
	"boji/internal/judge/jev/wire/openrouter"
)

func liveKey(t *testing.T, gate string) string {
	t.Helper()
	if os.Getenv(gate) != "1" {
		t.Skipf("set %s=1 to spend money on the jev, opus and fable arms", gate)
	}
	key, err := jev.Key(filepath.Join("..", "..", ".env"))
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	return key
}

func TestLiveProbeOverTheFirstHeldOutCases(t *testing.T) {
	key := liveKey(t, "BOJI_LIVE_COST_PROBE")
	count := 2
	if raw := os.Getenv("BOJI_LIVE_COST_CASES"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("BOJI_LIVE_COST_CASES: %v", err)
		}
		count = parsed
	}
	battery, _, err := benchapi.GateBattery()
	if err != nil {
		t.Fatalf("GateBattery: %v", err)
	}
	_, heldOut := halves(t)
	if count > len(heldOut) {
		count = len(heldOut)
	}
	probe := heldOut[:count]

	ctx := context.Background()
	point := publishedPoint(t)
	jevArm, err := runJev(ctx, key, probe, battery, point)
	if err != nil {
		t.Fatalf("the jev arm stopped: %v", err)
	}
	opusArm, err := runModel(ctx, key, ModelOpus, probe, battery, point)
	if err != nil {
		t.Fatalf("the opus arm stopped: %v", err)
	}
	fableArm, err := runModel(ctx, key, ModelFable, probe, battery, point)
	if err != nil {
		t.Fatalf("the fable arm stopped: %v", err)
	}
	for _, arm := range []ArmResult{jevArm, opusArm, fableArm} {
		t.Logf("%s: %d calls, %d correct, %d input tokens, $%.6f, p50 %.0f ms, model %s",
			arm.Arm, len(arm.Cases), arm.CorrectCount, arm.TotalInputTokens, arm.Total.Money, medianLatency(arm), modelOf(arm))
	}
}

func TestLiveEveryArmOverTheHeldOutHalf(t *testing.T) {
	key := liveKey(t, "BOJI_LIVE_COST")
	result, err := Run(context.Background(), key)
	if err != nil {
		t.Fatalf("the run stopped: %v", err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("no hostname for the conditions line: %v", err)
	}
	body := Render(result, report.Conditions{
		Machine:        host,
		CredentialKind: "key",
		Wire:           openrouter.Name,
		Date:           result.GeneratedAt.Format("2006-01-02"),
	})
	path := Filename(result.GeneratedAt)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	for _, arm := range result.Arms {
		t.Logf("%s: %d of %d correct, %d blocks caught, %d false blocks, $%.6f",
			arm.Arm, arm.CorrectCount, len(arm.Cases), arm.CaughtBlocks, arm.FalseBlocks, arm.Total.Money)
	}
	for _, pair := range result.Pairs {
		t.Logf("%s vs %s: %+d, p %.4f, separated %v", pair.Left, pair.Right, pair.Difference, pair.P, pair.SeparatedAt05)
	}
	t.Logf("total spend $%.6f, wrote bench/cost/%s", result.Total.Money, path)
}
