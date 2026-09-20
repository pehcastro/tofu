package recall_test

import (
	"fmt"
	"strings"
	"testing"

	"tofu/internal/recall"
)

func overflowing(bands recall.Bands, cfg recall.Config) recall.Conversation {
	conversation := recall.Conversation{Instructions: strings.Repeat("the brief. ", 100)}
	body := bands.Target() * cfg.BytesPerThousandTokens / 1000 / 8
	for step := 1; step <= 20; step++ {
		conversation.Entries = append(conversation.Entries,
			recall.Entry{Step: step, Text: fmt.Sprintf("step %d plan", step)},
			recall.Entry{
				Step:         step,
				Tool:         "read",
				SupersedeKey: fmt.Sprintf(`read {"path":"src/file%d.ts"}`, step%3),
				Text:         fmt.Sprintf("result of step %d: ", step) + strings.Repeat("x", body),
			})
	}
	oversized := bands.Recent * cfg.BytesPerThousandTokens / 1000 / 4
	conversation.Entries = append(conversation.Entries,
		recall.Entry{
			Step:         21,
			Tool:         "bash",
			SupersedeKey: `bash {"command":"pnpm test"}`,
			Text:         "first result of step 21: " + strings.Repeat("y", oversized),
		},
		recall.Entry{
			Step:         21,
			Tool:         "bash",
			SupersedeKey: `bash {"command":"pnpm test"}`,
			Text:         "second result of step 21: " + strings.Repeat("z", oversized),
		})
	return conversation
}

func TestAConversationOverTheCeilingCompactsAndEveryDroppedByteComesBackWhole(t *testing.T) {
	cfg := shippedConfig(t)
	bands := recall.ShippedBands()
	store := recall.NewStore(t.TempDir())
	before := overflowing(bands, cfg)

	occupancy := recall.Measure(cfg, bands, before)
	if occupancy.Total() <= bands.Target() {
		t.Fatalf("the fixture does not overflow: %d tokens against a %d target", occupancy.Total(), bands.Target())
	}

	after, drops, err := recall.Compact(store, cfg, bands, before)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(drops) == 0 {
		t.Fatal("a conversation over the target dropped nothing")
	}
	compacted := recall.Measure(cfg, bands, after)
	if compacted.Total() > bands.Target() {
		t.Fatalf("after compaction %d tokens still exceed the %d target", compacted.Total(), bands.Target())
	}
	t.Logf("%d tokens over %d, dropped %d results, now %d tokens", occupancy.Total(), bands.Target(), len(drops), compacted.Total())

	for i, drop := range drops {
		back, err := store.Fetch(drop.Handle)
		if err != nil {
			t.Fatalf("fetch %s: %v", drop.Handle, err)
		}
		at := dropIndex(t, before, drop)
		original := before.Entries[at].Text
		if string(back) != original {
			t.Fatalf("drop %d of %d: %d bytes came back where %d went in", i, len(drops), len(back), len(original))
		}
		if !strings.Contains(after.Entries[at].Text, drop.Handle) {
			t.Fatalf("drop %d left no handle in the conversation", i)
		}
	}
	t.Logf("all %d dropped results fetched back byte for byte, %s first", len(drops), drops[0].Reason)
}

func dropIndex(t *testing.T, conversation recall.Conversation, drop recall.Drop) int {
	t.Helper()
	for i, entry := range conversation.Entries {
		if entry.Step == drop.Step && entry.Tool == drop.Tool && len(entry.Text) == drop.Bytes {
			return i
		}
	}
	t.Fatalf("no entry matches drop %+v", drop)
	return -1
}

func TestCompactionNeverTouchesTheNewestStepOrTheIdentityBand(t *testing.T) {
	cfg := shippedConfig(t)
	bands := recall.ShippedBands()
	before := overflowing(bands, cfg)
	newest := before.Entries[len(before.Entries)-1].Step

	after, drops, err := recall.Compact(recall.NewStore(t.TempDir()), cfg, bands, before)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if after.Instructions != before.Instructions {
		t.Fatal("compaction rewrote the identity band")
	}
	for _, drop := range drops {
		if drop.Step == newest {
			t.Fatalf("compaction dropped a result from the newest step %d", newest)
		}
	}
	for i, entry := range after.Entries {
		if entry.Step != newest {
			continue
		}
		if entry.Text != before.Entries[i].Text {
			t.Fatalf("entry %d of the newest step %d was rewritten", i, newest)
		}
	}
	recentTokens := recall.Measure(cfg, bands, after).Recent
	if recentTokens != recall.Measure(cfg, bands, before).Recent {
		t.Fatalf("the recent band changed from %d to %d tokens", recall.Measure(cfg, bands, before).Recent, recentTokens)
	}
	t.Logf("newest step %d untouched, identity untouched, %d drops all older", newest, len(drops))
}

func TestASupersededResultIsDroppedBeforeAnOlderUniqueOne(t *testing.T) {
	cfg := recall.Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 8}
	bands := recall.Bands{Identity: 1, Facts: 1, WorkingSet: 1, Recent: 100}
	conversation := recall.Conversation{Entries: []recall.Entry{
		{Step: 1, Tool: "read", SupersedeKey: "read a", Text: strings.Repeat("a", 400)},
		{Step: 2, Tool: "bash", SupersedeKey: "bash b", Text: strings.Repeat("b", 400)},
		{Step: 3, Tool: "read", SupersedeKey: "read a", Text: strings.Repeat("c", 400)},
		{Step: 4, Tool: "read", Text: strings.Repeat("d", 50)},
	}}

	_, drops, err := recall.Compact(recall.NewStore(t.TempDir()), cfg, bands, conversation)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(drops) == 0 {
		t.Fatal("nothing was dropped")
	}
	if drops[0].Step != 1 || drops[0].Reason != recall.DroppedSuperseded {
		t.Fatalf("first drop = step %d %s, want step 1 superseded by the step 3 read of the same path", drops[0].Step, drops[0].Reason)
	}
}

func TestAConversationUnderTheTargetIsReturnedUntouched(t *testing.T) {
	cfg := shippedConfig(t)
	bands := recall.ShippedBands()
	conversation := recall.Conversation{Entries: []recall.Entry{{Step: 1, Tool: "read", Text: strings.Repeat("x", 4096)}}}

	after, drops, err := recall.Compact(recall.NewStore(t.TempDir()), cfg, bands, conversation)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(drops) != 0 || after.Entries[0].Text != conversation.Entries[0].Text {
		t.Fatalf("a conversation under the target was compacted: %d drops", len(drops))
	}
}
