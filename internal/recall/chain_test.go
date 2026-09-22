package recall

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/konst"
)

type recordedCall struct {
	Tool          string          `json:"tool"`
	Args          json.RawMessage `json:"args,omitempty"`
	Command       string          `json:"command,omitempty"`
	RenderedBytes int             `json:"rendered_bytes"`
}

type recordedStep struct {
	Index           int            `json:"index"`
	AssistantText   string         `json:"assistant_text,omitempty"`
	CacheReadTokens int            `json:"cache_read_tokens"`
	ToolCalls       []recordedCall `json:"tool_calls,omitempty"`
}

type recordedTurn struct {
	ID    string         `json:"id"`
	Task  string         `json:"task"`
	Steps []recordedStep `json:"steps"`
}

func readRecordedTurn(t *testing.T) (Config, recordedTurn) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "bench", "recall", "testdata", "recorded-turn.json"))
	if err != nil {
		t.Skipf("the recorded turn is not readable from here: %v", err)
	}
	var turn recordedTurn
	if err := json.Unmarshal(data, &turn); err != nil {
		t.Fatalf("the recorded turn does not parse: %v", err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("load the elide library: %v", err)
	}
	return cfg, turn
}

func recordedSource(call recordedCall) string {
	var args toolArgs
	if json.Unmarshal(call.Args, &args) != nil {
		return call.Tool
	}
	return cmp.Or(args.Path, args.Handle, args.Pattern, args.Command, call.Tool)
}

func entriesOf(step recordedStep) []Entry {
	assistant := step.AssistantText
	for _, call := range step.ToolCalls {
		assistant += "\n" + call.Tool + " " + string(call.Args)
	}
	var entries []Entry
	if assistant != "" {
		entries = append(entries, Entry{Step: step.Index, Text: assistant})
	}
	for _, call := range step.ToolCalls {
		entries = append(entries, Entry{
			Step:         step.Index,
			Tool:         call.Tool,
			SupersedeKey: call.Tool + " " + string(call.Args),
			Text:         recordedBody(call),
		})
	}
	return entries
}

func recordedBody(call recordedCall) string {
	stand := call.Tool + " result of " + call.Command + ", length recorded, body not\n"
	if len(stand) >= call.RenderedBytes {
		return stand[:call.RenderedBytes]
	}
	return stand + strings.Repeat(".", call.RenderedBytes-len(stand))
}

type chainArm struct {
	name           string
	carry          func(*Store, Config, Conversation) (Carry, error)
	forwardFacts   bool
	distilEachStep bool
}

type chainRun struct {
	forks       int
	refetches   int
	blind       int
	blindBytes  int
	bytes       int
	carryTokens int
	factTokens  int
	factLines   int
	stepTokens  int
}

func replayChain(t *testing.T, cfg Config, bands Bands, turn recordedTurn, arm chainArm) chainRun {
	t.Helper()
	store := NewStore(t.TempDir())
	instructions := turn.Task + strings.Repeat(".", turn.Steps[0].CacheReadTokens*cfg.BytesPerThousandTokens/1000)
	conversation := Conversation{Instructions: instructions}

	var run chainRun
	var carryText string
	seen, carried := make(map[string]bool), make(map[string]bool)
	for _, step := range turn.Steps {
		for _, call := range step.ToolCalls {
			source := recordedSource(call)
			if carried[source] {
				run.refetches++
				run.bytes += call.RenderedBytes
				if !strings.Contains(carryText, source) {
					run.blind++
					run.blindBytes += call.RenderedBytes
				}
			}
			seen[source] = true
		}
		conversation.Entries = append(conversation.Entries, entriesOf(step)...)
		if arm.distilEachStep {
			sheet, _, err := Distil(store, conversation, konst.FactSignpostBytes)
			if err != nil {
				t.Fatalf("%s: distil at step %d: %v", arm.name, step.Index, err)
			}
			conversation.Facts = sheet
			measured := Measure(cfg, bands, conversation)
			run.factTokens, run.factLines = max(run.factTokens, measured.Facts), len(sheet)
			run.stepTokens = measured.Total()
		}
		if arm.carry == nil || !Crossed(cfg, bands, conversation) {
			continue
		}
		carry, err := arm.carry(store, cfg, conversation)
		if err != nil {
			t.Fatalf("%s: carry at step %d: %v", arm.name, step.Index, err)
		}
		conversation = Conversation{Instructions: instructions + "\n" + carry.Text}
		if arm.forwardFacts {
			conversation.Facts = carry.Facts
		}
		carried, carryText = maps.Clone(seen), carry.Text
		run.forks++
		run.carryTokens = cfg.Tokens(carry.Text)
		run.factTokens, run.factLines = max(run.factTokens, cfg.Tokens(strings.Join(carry.Facts, "\n"))), len(carry.Facts)
	}
	return run
}

func stepListCarry(store *Store, cfg Config, c Conversation) (Carry, error) {
	var carry Carry
	text := &strings.Builder{}
	text.WriteString(carryPreamble)
	for _, entry := range c.Entries {
		if entry.Tool == "" {
			continue
		}
		handle := entry.Handle
		if handle == "" && len(entry.Text) >= cfg.CompactFloorBytes {
			put, err := store.put([]byte(entry.Text))
			if err != nil {
				return Carry{}, err
			}
			handle = put
		}
		result := CarriedResult{Tool: entry.Tool, Key: entry.SupersedeKey, Bytes: len(entry.Text), Handle: handle}
		fmt.Fprintf(text, "  step %d %s: %d bytes", entry.Step, oneLine(entry.SupersedeKey, konst.CarrySignpostBytes), result.Bytes)
		if handle != "" {
			fmt.Fprintf(text, ", artifact %s", handle)
		}
		result.Signpost = oneLine(entry.Text, konst.CarrySignpostBytes)
		fmt.Fprintf(text, "\n    it came back: %s\n", result.Signpost)
		carry.Results = append(carry.Results, result)
	}
	text.WriteString(carryLastWord)
	text.WriteString(lastWord(c))
	carry.Text = text.String()
	return carry, nil
}

func TestAcrossAChainOfForksTheFactSheetNamesWhatTheStepListLost(t *testing.T) {
	cfg, turn := readRecordedTurn(t)
	bands := BandsOf(20000)

	before := replayChain(t, cfg, bands, turn, chainArm{name: "step list, one session deep", carry: stepListCarry})
	after := replayChain(t, cfg, bands, turn, chainArm{name: "fact sheet", carry: HandleCarry, forwardFacts: true})
	long := replayChain(t, cfg, bands, turn, chainArm{name: "fact sheet, long signposts", carry: DistilledCarry, forwardFacts: true})

	t.Logf("recorded turn %s, %d steps, ceiling 20000, target %d tokens", turn.ID, len(turn.Steps), bands.Target())
	for _, run := range []struct {
		name string
		run  chainRun
	}{{"step list", before}, {"fact sheet", after}, {"long facts", long}} {
		t.Logf("%-10s %2d forks, the next session went back to %2d sources it already had, %2d of them named in no carry, %6d bytes re-fetched blind of %6d, last carry %4d tokens, facts band %3d tokens in %d lines",
			run.name, run.run.forks, run.run.refetches, run.run.blind, run.run.blindBytes, run.run.bytes, run.run.carryTokens, run.run.factTokens, run.run.factLines)
	}
	if after.blind >= before.blind {
		t.Fatalf("the fact sheet left %d sources unnamed against %d for the step list", after.blind, before.blind)
	}
	if after.blindBytes >= before.blindBytes {
		t.Fatalf("the fact sheet re-fetched %d bytes blind against %d for the step list", after.blindBytes, before.blindBytes)
	}
	if after.factTokens == 0 {
		t.Fatal("the facts band reserves space and the run put nothing in it")
	}
}

func TestCompactionCannotReachALowTargetBecauseItNeverDropsWhatTheModelSaid(t *testing.T) {
	cfg, turn := readRecordedTurn(t)
	bands := BandsOf(20000)
	store := NewStore(t.TempDir())
	conversation := Conversation{Instructions: turn.Task}

	compactions := 0
	for _, step := range turn.Steps {
		conversation.Entries = append(conversation.Entries, entriesOf(step)...)
		after, drops, err := Compact(store, cfg, bands, conversation)
		if err != nil {
			t.Fatalf("compact at step %d: %v", step.Index, err)
		}
		conversation = after
		if len(drops) > 0 {
			compactions++
		}
	}

	said, notices, whole := 0, 0, 0
	for _, entry := range conversation.Entries {
		switch {
		case entry.Tool == "":
			said += cfg.Tokens(entry.Text)
		case AlreadyDropped(entry.Text):
			notices += cfg.Tokens(entry.Text)
		default:
			whole += cfg.Tokens(entry.Text)
		}
	}
	ended := Measure(cfg, bands, conversation).Total()
	t.Logf("%d compactions against a %d token target left %d tokens: %d in what the model itself said, which compaction never drops, %d in drop notices, %d in results still held whole, %d in the instructions",
		compactions, bands.Target(), ended, said, notices, whole, cfg.Tokens(conversation.Instructions))
	if ended <= bands.Target() {
		t.Fatalf("compaction reached the %d token target and ended at %d, so the floor this test names does not exist", bands.Target(), ended)
	}
	if said*2 < ended {
		t.Fatalf("what the model said is %d of the %d tokens left, so the floor is not set by assistant text after all", said, ended)
	}
}

func TestResumingFromTheFactSheetCostsLessThanReplayingTheBranchItWouldRewindTo(t *testing.T) {
	cfg, turn := readRecordedTurn(t)
	bands := ShippedBands()
	store := NewStore(t.TempDir())
	conversation := Conversation{Instructions: turn.Task}

	checkpoint := len(turn.Steps) / 2
	for _, step := range turn.Steps[:checkpoint] {
		conversation.Entries = append(conversation.Entries, entriesOf(step)...)
	}
	sheet, _, err := Distil(store, conversation, konst.FactSignpostBytes)
	if err != nil {
		t.Fatalf("distil at the checkpoint: %v", err)
	}

	rewound := Measure(cfg, bands, conversation).Total()
	fresh := cfg.Tokens(turn.Task) + cfg.Tokens(strings.Join(sheet, "\n"))
	t.Logf("at step %d of %d: rewinding to the checkpoint re-sends %d tokens, an isolated spawn against the fact sheet of the same checkpoint sends %d in %d lines, %d percent of the rewind",
		checkpoint, len(turn.Steps), rewound, fresh, len(sheet), fresh*100/rewound)
	if fresh >= rewound {
		t.Fatalf("the isolated spawn costs %d tokens against %d for the rewind, so the cheap arm is not cheap", fresh, rewound)
	}
}

func TestTheFactsBandFillsOnATurnThatNeverForksAndStaysInsideTheCapItReserves(t *testing.T) {
	cfg, turn := readRecordedTurn(t)
	bands := ShippedBands()
	if bands.Facts == 0 {
		t.Fatal("the facts band reserves nothing, so nothing it holds can survive a compaction or a fork")
	}

	run := replayChain(t, cfg, bands, turn, chainArm{name: "distil every step", distilEachStep: true})

	t.Logf("recorded turn %s at the shipped %d token target: the fact sheet ends at %d lines and %d tokens against a %d token facts cap, and the turn ends at %d tokens",
		turn.ID, bands.Target(), run.factLines, run.factTokens, bands.Facts, run.stepTokens)
	if run.factTokens == 0 {
		t.Fatal("44 steps of tool results distilled to nothing")
	}
	if run.factTokens > bands.Facts {
		t.Fatalf("the fact sheet of one recorded turn is %d tokens against the %d the band reserves, so the share in konst is set below what one turn produces",
			run.factTokens, bands.Facts)
	}
}
