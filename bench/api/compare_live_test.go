package api_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/bench/api"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/llm/cred"
	"tofu/internal/sys"
)

func jevSecret(t *testing.T, vendor cred.Vendor, variable string) string {
	t.Helper()
	sys.AllowLiveCredential(t)
	secret, err := jev.KeyFor(filepath.Join("..", "..", ".env"), variable)
	if err != nil {
		t.Fatalf("no %s credential: %v", vendor, err)
	}
	return strings.TrimSpace(secret)
}

func TestLiveTheSameBatteryThroughBothWires(t *testing.T) {
	if os.Getenv("TOFU_LIVE_COMPARE") != "1" {
		t.Skip("set TOFU_LIVE_COMPARE=1 to spend on both jev wires over the gate corpus")
	}

	throughOpenRouter, err := api.NewWire(jevSecret(t, cred.OpenRouter, jev.OpenRouterVariable))
	if err != nil {
		t.Fatalf("openrouter wire: %v", err)
	}
	direct, err := api.NewTypeSafeWire(jevSecret(t, cred.TypeSafe, jev.TypeSafeVariable))
	if err != nil {
		t.Fatalf("typesafe wire: %v", err)
	}

	const repsPerCase = 3
	comparisons, err := api.CompareWires(context.Background(), []api.NamedWire{
		{Name: "openrouter", Wire: throughOpenRouter},
		{Name: "typesafe", Wire: direct, DollarsPerInputToken: typesafe.DollarsPerInputToken},
	}, repsPerCase)
	if err != nil {
		t.Fatalf("comparing the wires: %v", err)
	}

	disagreed, calls := 0, 0
	totals := map[string]float64{}
	for _, comparison := range comparisons {
		if !comparison.Agree {
			disagreed++
		}
		for _, decision := range comparison.Decisions {
			calls += repsPerCase
			totals[decision.Wire] += decision.Cost * repsPerCase
			t.Logf("%-32s %-11s %-7s build %-12s risk %.2f median %6.0f ms %4d input tokens $%.9f per decision",
				comparison.Case, decision.Wire, decision.Verdict, decision.Build,
				decision.Risk, decision.MedianMS, decision.InputTokens, decision.Cost)
		}
	}
	t.Logf("%d cases, %d calls, %d disagreements", len(comparisons), calls, disagreed)
	for wire, spent := range totals {
		t.Logf("%s spent $%.6f", wire, spent)
	}
}
