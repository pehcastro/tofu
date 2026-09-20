package recall_test

import (
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/recall"
)

func factsConfig() recall.Config {
	return recall.Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 256}
}

func TestEveryKindOfResultBecomesOneFactLineNamingWhatWasAskedAndWhatCameBack(t *testing.T) {
	store := recall.NewStore(t.TempDir())
	conversation := recall.Conversation{Entries: []recall.Entry{
		{Step: 1, Tool: "read", SupersedeKey: `read {"path":"src/app.ts"}`, Text: "export const routes = []\n" + strings.Repeat("x", 4000)},
		{Step: 1, Tool: "bash", SupersedeKey: `bash {"command":"pnpm -r test"}`, Text: "1163 tests passed, vitest"},
		{Step: 2, Tool: "grep", SupersedeKey: `grep {"pattern":"func Decide"}`, Text: "internal/judge/policy.go:12"},
		{Step: 2, Tool: "artifact_fetch", SupersedeKey: `artifact_fetch {"handle":"a1b2"}`, Text: "bytes 0 to 200 of the test log"},
		{Step: 3, Text: "the routes are renamed"},
	}}

	sheet, kept, err := recall.Distil(store, conversation, konst.FactSignpostBytes)
	if err != nil {
		t.Fatalf("distil: %v", err)
	}
	if len(sheet) != 4 || len(kept) != 4 {
		t.Fatalf("four results of four kinds gave %d fact lines and %d kept results:\n%s", len(sheet), len(kept), strings.Join(sheet, "\n"))
	}
	for _, want := range []string{"src/app.ts", "pnpm -r test", "func Decide", "a1b2"} {
		if !strings.Contains(strings.Join(sheet, "\n"), want) {
			t.Fatalf("no fact line names %q:\n%s", want, strings.Join(sheet, "\n"))
		}
	}
	for _, line := range sheet {
		if !strings.Contains(line, " bytes") || !strings.Contains(line, ", artifact ") || !strings.Contains(line, "it came back: ") {
			t.Fatalf("a fact line does not say what was asked, how big it was and what came back: %q", line)
		}
	}
	for _, result := range kept {
		back, err := store.Fetch(result.Handle)
		if err != nil {
			t.Fatalf("the artifact a fact names does not read back: %v", err)
		}
		if len(back) != result.Bytes {
			t.Fatalf("the fact for %s claims %d bytes and its artifact holds %d", result.Key, result.Bytes, len(back))
		}
	}
	t.Logf("%s", strings.Join(sheet, "\n"))
}

func TestReadingTheSameSourceTwiceSupersedesItsFactRatherThanAddingASecond(t *testing.T) {
	store := recall.NewStore(t.TempDir())
	conversation := recall.Conversation{Entries: []recall.Entry{
		{Step: 1, Tool: "read", SupersedeKey: `read {"path":"src/app.ts"}`, Text: "the old body"},
		{Step: 4, Tool: "read", SupersedeKey: `read {"path":"src/app.ts"}`, Text: "the body after the edit"},
	}}

	sheet, _, err := recall.Distil(store, conversation, konst.FactSignpostBytes)
	if err != nil {
		t.Fatalf("distil: %v", err)
	}
	if len(sheet) != 1 {
		t.Fatalf("one file read twice gave %d fact lines:\n%s", len(sheet), strings.Join(sheet, "\n"))
	}
	if !strings.Contains(sheet[0], "the body after the edit") {
		t.Fatalf("the surviving fact is the stale one: %q", sheet[0])
	}

	conversation.Facts = sheet
	again, _, err := recall.Distil(store, conversation, konst.FactSignpostBytes)
	if err != nil {
		t.Fatalf("distil again: %v", err)
	}
	if len(again) != 1 || again[0] != sheet[0] {
		t.Fatalf("distilling the same conversation twice moved the sheet from %q to %v", sheet[0], again)
	}
}

func TestAFactSurvivesTheCompactionThatDropsTheResultItCameFrom(t *testing.T) {
	cfg := factsConfig()
	store := recall.NewStore(t.TempDir())
	body := "1163 tests passed, vitest\n" + strings.Repeat("detail\n", 400)
	conversation := recall.Conversation{Entries: []recall.Entry{
		{Step: 1, Tool: "bash", SupersedeKey: `bash {"command":"pnpm -r test"}`, Text: body},
		{Step: 2, Text: "now i know how the tests run"},
		{Step: 3, Text: "still working"},
	}}
	sheet, _, err := recall.Distil(store, conversation, konst.FactSignpostBytes)
	if err != nil {
		t.Fatalf("distil: %v", err)
	}
	conversation.Facts = sheet

	bands := recall.Bands{Identity: 1, Facts: 100, WorkingSet: 1, Recent: 10}
	after, drops, err := recall.Compact(store, cfg, bands, conversation)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(drops) != 1 {
		t.Fatalf("compaction dropped %d results, want the one result that was over the floor", len(drops))
	}
	for _, entry := range after.Entries {
		if strings.Contains(entry.Text, "1163 tests passed") {
			t.Fatal("the result compaction claims to have dropped is still in the conversation")
		}
	}
	if len(after.Facts) != 1 || !strings.Contains(after.Facts[0], "1163 tests passed") {
		t.Fatalf("the fact did not survive the compaction of the result it came from: %v", after.Facts)
	}
	back, err := store.Fetch(drops[0].Handle)
	if err != nil || !strings.Contains(string(back), "1163 tests passed") {
		t.Fatalf("the dropped result is not readable from the artifact the drop names: %v", err)
	}
	t.Logf("dropped %d bytes at step %d and kept: %s", drops[0].Bytes, drops[0].Step, after.Facts[0])
}

func TestAFactSurvivesAChainOfForksThatCarriesNothingButTheTextOfTheCarry(t *testing.T) {
	cfg := factsConfig()
	store := recall.NewStore(t.TempDir())
	first := recall.Conversation{Instructions: "rename the field", Entries: []recall.Entry{
		{Step: 1, Tool: "bash", SupersedeKey: `bash {"command":"pnpm -r test"}`, Text: "1163 tests passed, vitest"},
		{Step: 1, Text: "the tests run with pnpm"},
	}}

	carried := first
	for session := range 3 {
		carry, err := recall.HandleCarry(store, cfg, carried)
		if err != nil {
			t.Fatalf("carry %d: %v", session, err)
		}
		name := string(rune('a' + session))
		carried = recall.Conversation{Instructions: first.Instructions, Entries: []recall.Entry{
			{Step: 1, Text: carry.Text},
			{Step: 1, Tool: "read", SupersedeKey: `read {"path":"src/` + name + `.ts"}`, Text: "a file read in session " + name},
			{Step: 2, Text: "session " + name + " did its part"},
		}}
	}
	carry, err := recall.HandleCarry(store, cfg, carried)
	if err != nil {
		t.Fatalf("the carry after the chain: %v", err)
	}

	if !strings.Contains(carry.Text, "pnpm -r test") {
		t.Fatalf("after three forks the first session's source is named nowhere in the carry:\n%s", carry.Text)
	}
	for session := range 3 {
		if !strings.Contains(carry.Text, "src/"+string(rune('a'+session))+".ts") {
			t.Fatalf("the carry lost what session %d read:\n%s", session, carry.Text)
		}
	}
	if len(carry.Facts) != 4 {
		t.Fatalf("the fact sheet after three forks holds %d lines, want the four sources the lineage read:\n%s", len(carry.Facts), strings.Join(carry.Facts, "\n"))
	}
	t.Logf("%s", carry.Text)
}

func TestTheFactsBandReservesSpaceAndTheCapsStayInProportion(t *testing.T) {
	if konst.BandFactsShare == 0 {
		t.Fatal("the facts band reserves nothing again, so a result worth keeping has nowhere durable to go")
	}
	shipped := recall.ShippedBands()
	if shipped.Facts != konst.ContextCeilingTokens*konst.BandFactsShare/konst.BandShareWhole {
		t.Fatalf("the shipped facts band is %d tokens and the share says %d", shipped.Facts, konst.BandFactsShare)
	}
	if recall.BandsOf(20000).Facts == 0 {
		t.Fatal("at a 20000 token ceiling the facts band rounds to nothing")
	}
	t.Logf("shipped bands %+v, target %d", shipped, shipped.Target())
}
