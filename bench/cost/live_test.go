package cost

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	benchapi "tofu/bench/api"
	"tofu/bench/report"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
)

const repoRoot = "../.."

func liveKey(t *testing.T, gate string) string {
	t.Helper()
	if os.Getenv(gate) != "1" {
		t.Skipf("set %s=1 to spend money on the jev, opus and fable arms", gate)
	}
	jev.AllowLiveCredential(t)
	key, err := jev.Key(filepath.Join(repoRoot, ".env"))
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	return key
}

func TestLiveProbeOverTheFirstHeldOutCases(t *testing.T) {
	key := liveKey(t, "TOFU_LIVE_COST_PROBE")
	count := 2
	if raw := os.Getenv("TOFU_LIVE_COST_CASES"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("TOFU_LIVE_COST_CASES: %v", err)
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
	pol, _ := shippedGate(t)
	jevArm, err := runJev(ctx, key, probe, battery, pol)
	if err != nil {
		t.Fatalf("the jev arm stopped: %v", err)
	}
	t.Logf("jev: %d calls, %d correct, %d input tokens, $%.6f, p50 %.0f ms, model %s",
		len(jevArm.Cases), jevArm.CorrectCount, jevArm.TotalInputTokens, jevArm.Total.Money, medianLatency(jevArm), modelOf(jevArm))
}

func TestLiveEveryArmOverTheHeldOutHalf(t *testing.T) {
	key := liveKey(t, "TOFU_LIVE_COST")
	t.Chdir(repoRoot)
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
	path := filepath.Join("bench", "cost", Filename(result.GeneratedAt))
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
	t.Logf("total spend $%.6f, wrote %s and bench/cost/%s", result.Total.Money, path, result.AnswersFile)
}
