package recall

import (
	"testing"

	"tofu/internal/llm"
	rc "tofu/internal/recall"
)

func TestACodexTurnIsNotBilledTwiceForItsCacheReads(t *testing.T) {
	cfg, err := rc.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	recorded := Session{
		ID:   "turn-18d732c5a61bd6ac",
		Wire: llm.WireCodex,
		Task: "read the repository and say what it does",
		Steps: []SessionStep{
			{Index: 1, AssistantText: "looking at the tree", PromptTokens: 4066},
			{Index: 2, AssistantText: "reading the entry point", PromptTokens: 4169},
			{Index: 3, AssistantText: "reading the loop", PromptTokens: 4242, CacheReadTokens: 3968},
			{Index: 4, AssistantText: "reading the wires", PromptTokens: 5293, CacheReadTokens: 4096},
			{Index: 5, AssistantText: "writing the answer", PromptTokens: 8008, CacheReadTokens: 5120},
		},
	}
	result := replay(t, cfg, rc.ShippedBands(), recorded, armNothing)

	for index, want := range []int{4066, 4169, 4242, 5293, 8008} {
		if got := result.Steps[index].RecordedTokens; got != want {
			t.Errorf("step %d reads %d billed tokens, want %d", index+1, got, want)
		}
	}
}

func TestAnAnthropicTurnStillBillsItsCacheReadsOnTopOfItsPrompt(t *testing.T) {
	cfg, session := recordedTurn(t)
	result := replay(t, cfg, rc.ShippedBands(), session, armNothing)

	first := session.Steps[0]
	if want := first.PromptTokens + first.CacheReadTokens; result.Steps[0].RecordedTokens != want {
		t.Fatalf("step 1 reads %d billed tokens, want %d", result.Steps[0].RecordedTokens, want)
	}
	t.Logf("%s reads %d billed tokens at step 1, prompt %d plus cache read %d",
		session.ID, result.Steps[0].RecordedTokens, first.PromptTokens, first.CacheReadTokens)
}
