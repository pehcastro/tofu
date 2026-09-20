package recall_test

import (
	"strings"
	"testing"

	"tofu/internal/recall"
)

func TestTheCarryNamesEveryResultAndHoldsEachOneWholeInTheStore(t *testing.T) {
	cfg := recall.Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 8}
	store := recall.NewStore(t.TempDir())
	conversation := recall.Conversation{Entries: []recall.Entry{
		{Step: 1, Text: "i will read the app"},
		{Step: 1, Tool: "read", SupersedeKey: `read {"path":"src/app.ts"}`, Text: strings.Repeat("a", 400)},
		{Step: 2, Tool: "bash", SupersedeKey: `bash {"command":"pnpm test"}`, Text: strings.Repeat("b", 400)},
		{Step: 2, Text: "the routes are renamed and the tests pass"},
	}}

	carry, err := recall.HandleCarry(store, cfg, conversation)
	if err != nil {
		t.Fatalf("handle carry: %v", err)
	}
	if len(carry.Results) != 2 {
		t.Fatalf("the carry names %d results, want both tool results", len(carry.Results))
	}
	for _, result := range carry.Results {
		if !strings.Contains(carry.Text, result.Handle) {
			t.Fatalf("the %s result is in the carry but its handle %s is not in the text the new session receives", result.Tool, result.Handle)
		}
		back, err := store.Fetch(result.Handle)
		if err != nil {
			t.Fatalf("fetch %s: %v", result.Handle, err)
		}
		if len(back) != result.Bytes {
			t.Fatalf("artifact %s came back %d bytes where %d went in", result.Handle, len(back), result.Bytes)
		}
	}
	if !strings.Contains(carry.Text, "the routes are renamed and the tests pass") {
		t.Fatal("the carry drops the last thing the ended session said or did, which is the only statement of where the work stands")
	}
	if strings.Contains(carry.Text, strings.Repeat("a", 400)) {
		t.Fatal("the carry pastes a result body instead of its handle, so it carries the cost it exists to avoid")
	}
}

func TestCrossedIsTheTargetAndNotTheCeiling(t *testing.T) {
	cfg := recall.Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 8}
	bands := recall.Bands{Identity: 10, Facts: 0, WorkingSet: 100, Recent: 100}
	under := recall.Conversation{Entries: []recall.Entry{{Step: 1, Text: strings.Repeat("a", 100)}}}
	over := recall.Conversation{Entries: []recall.Entry{
		{Step: 1, Text: strings.Repeat("a", 200000)},
		{Step: 2, Text: "now"},
	}}

	if recall.Crossed(cfg, bands, under) {
		t.Fatal("a conversation inside the target was called crossed")
	}
	if !recall.Crossed(cfg, bands, over) {
		t.Fatal("a conversation past the target was not called crossed")
	}
}
