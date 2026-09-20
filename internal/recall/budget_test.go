package recall_test

import (
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/recall"
)

func shippedConfig(t *testing.T) recall.Config {
	t.Helper()
	cfg, err := recall.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestTheRecentBandHoldsTheNewestStepWholeEvenWhenItIsOversized(t *testing.T) {
	cfg := recall.Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 8}
	bands := recall.Bands{Identity: 10, Facts: 10, WorkingSet: 100, Recent: 10}
	conversation := recall.Conversation{
		Entries: []recall.Entry{
			{Step: 1, Tool: "read", Text: strings.Repeat("a", 500)},
			{Step: 2, Tool: "read", Text: strings.Repeat("b", 500)},
			{Step: 2, Tool: "read", Text: strings.Repeat("c", 500)},
		},
	}

	occupancy := recall.Measure(cfg, bands, conversation)
	if occupancy.Recent != 1000+2*konst.MessageFramingTokens {
		t.Fatalf("recent band = %d tokens, want both entries of step 2, 1000 tokens of text and the framing the wire puts around each one", occupancy.Recent)
	}
	if occupancy.WorkingSet != 500+konst.MessageFramingTokens {
		t.Fatalf("working set = %d tokens, want the single step 1 entry, 500 tokens of text and its framing", occupancy.WorkingSet)
	}
}
