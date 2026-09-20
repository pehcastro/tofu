package turn

import (
	"context"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
)

func TestAModelWithNoRecordedWindowStillCompactsAtTheOperatingCeiling(t *testing.T) {
	config, _ := longTurnConfig(t)
	unknown, err := recall.BudgetFor("a model no catalog records", 0)
	if err != nil {
		t.Fatalf("budget for an unrecorded model: %v", err)
	}
	if unknown.CeilingTokens != konst.ContextCeilingTokens {
		t.Fatalf("an unrecorded model runs at a %d token ceiling, want the %d tofu operates under whatever the model is",
			unknown.CeilingTokens, konst.ContextCeilingTokens)
	}
	config.Budget = unknown

	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	measured := 0
	for _, step := range row.Steps {
		if step.Occupancy != nil {
			measured++
		}
	}
	if measured == 0 {
		t.Fatalf("nothing was measured across %d steps, so an unrecorded window stopped the budget watching", len(row.Steps))
	}
	t.Logf("%d steps measured under %s", measured, unknown.Record())
}

func TestAForkCarriesWhatItFoundRatherThanAListOfByteCounts(t *testing.T) {
	_, row, ended, _ := forkingTurn(t)
	if len(ended) == 0 {
		t.Fatal("the turn never forked, so there is no carry to read")
	}
	var fork *Fork
	for _, session := range append(ended, row) {
		for _, step := range session.Steps {
			if step.Fork != nil {
				fork = step.Fork
			}
		}
	}
	if fork == nil {
		t.Fatal("no step recorded the fork it made")
	}
	if len(fork.Carry.Facts) == 0 {
		t.Fatalf("the carry lists %d results and no facts, so the new session is given byte counts alone: %q",
			len(fork.Carry.Results), fork.Carry.Text)
	}
	if !strings.Contains(fork.Carry.Text, "it came back: ") {
		t.Fatalf("the carry never says what a result came back with: %q", fork.Carry.Text)
	}
	t.Logf("%s", fork.Carry.Text)
}

func TestTheFirstRequestAfterAForkCarriesTheSignpostsRatherThanTheResults(t *testing.T) {
	model, _, _, _ := forkingTurn(t)
	if len(model.requests) == 0 {
		t.Fatal("the model was never asked")
	}
	var afterFork []llm.Message
	for _, request := range model.requests {
		for _, message := range request {
			if afterFork == nil && strings.Contains(message.Content, "it came back: ") {
				afterFork = request
			}
		}
	}
	if afterFork == nil {
		t.Fatal("no request the model was given carries the carry, so the fork never reached the model")
	}
	whole := strings.Repeat("x", longTurnResultBytes)
	for _, message := range afterFork {
		if strings.Contains(message.Content, whole) {
			t.Fatalf("the first request after the fork still carries a whole %d byte result", longTurnResultBytes)
		}
	}
	t.Logf("the request after the fork carries %d messages and no whole result", len(afterFork))
}
