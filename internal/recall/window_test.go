package recall_test

import (
	"strings"
	"testing"
	"time"

	"tofu/internal/recall"
	"tofu/internal/session"
)

func budgetFor(t *testing.T, model string) recall.Budget {
	t.Helper()
	budget, err := recall.BudgetFor(model)
	if err != nil {
		t.Fatalf("budget for %s: %v", model, err)
	}
	return budget
}

func TestTheTargetComesFromTheWindowOfTheModelTheTurnRunsOn(t *testing.T) {
	for _, want := range []struct {
		model  string
		window int
		target int
	}{
		{"anthropic/claude-opus-5", 1000000, 200000},
		{"openai/gpt-5.5", 400000, 80000},
		{"anthropic/claude-haiku-4-5-20251001", 200000, 40000},
		{"claude-opus-5", 1000000, 200000},
	} {
		budget := budgetFor(t, want.model)
		if !budget.Automatic {
			t.Fatalf("%s has a recorded window and its budget still reads as unknown", want.model)
		}
		if budget.CeilingTokens != want.window {
			t.Fatalf("%s read a %d token window, want %d", want.model, budget.CeilingTokens, want.window)
		}
		if got := budget.Bands.Target(); got != want.target {
			t.Fatalf("%s got a %d token target from a %d token window, want %d", want.model, got, want.window, want.target)
		}
	}
}

func TestTheBandsKeepTheProportionsTheMeasurementEstablished(t *testing.T) {
	shipped := recall.ShippedBands()
	if shipped != (recall.Bands{Identity: 4000, Facts: 0, WorkingSet: 16000, Recent: 30000}) {
		t.Fatalf("the bands at the 250000 token ceiling are %+v, and BOJI-119 measured 4000, 0, 16000, 30000 from a real turn", shipped)
	}
	if got := recall.BandsOf(1000000); got != (recall.Bands{Identity: 16000, Facts: 0, WorkingSet: 64000, Recent: 120000}) {
		t.Fatalf("a million token window gives %+v, want every band four times the shipped one", got)
	}
	if got := recall.BandsOf(100000000).Target(); got != 250000 {
		t.Fatalf("a window far past the hard ceiling gave a %d token target, want the 250000 wall", got)
	}
}

func TestAModelWithNoRecordedWindowIsNeverCompactedOnATokenCount(t *testing.T) {
	budget := budgetFor(t, "anthropic/claude-sonnet-5")
	if budget.Automatic {
		t.Fatal("a model with no recorded context window was given an automatic budget, so its first step can be truncated on a guess")
	}
	cfg := recall.Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 8}
	huge := recall.Conversation{Entries: []recall.Entry{
		{Step: 1, Tool: "read", Text: strings.Repeat("a", 4000000)},
		{Step: 2, Text: "now"},
	}}

	if !recall.Crossed(cfg, budget.Bands, huge) {
		t.Fatal("four million bytes did not cross the shipped bands, so this proves nothing about the unknown window")
	}
	if budget.Crossed(cfg, huge) {
		t.Fatal("an unknown window crossed its budget, so the automatic compaction would fire on a model nothing knows the limit of")
	}
	if !strings.Contains(budget.Record(), "no context window is recorded for anthropic/claude-sonnet-5") {
		t.Fatalf("the record does not say why the budget is off:\n%s", budget.Record())
	}
}

func TestTheCeilingIsSetForOneRunAndTheRecordSaysWhereTheNumberCameFrom(t *testing.T) {
	fromCatalog := budgetFor(t, "claude-opus-5")
	if fromCatalog.CeilingTokens != 1000000 {
		t.Fatalf("with nothing set, claude-opus-5 read a %d token ceiling, want the recorded 1000000", fromCatalog.CeilingTokens)
	}

	t.Setenv(recall.CeilingVariable, "20000")
	set := budgetFor(t, "claude-opus-5")
	if set.CeilingTokens != 20000 || set.Bands.Target() != 4000 {
		t.Fatalf("%s=20000 gave a %d token ceiling and a %d token target, want 20000 and 4000",
			recall.CeilingVariable, set.CeilingTokens, set.Bands.Target())
	}
	if !strings.Contains(set.Source, recall.CeilingVariable) || !strings.Contains(set.Record(), "20000") {
		t.Fatalf("the record does not say where the 20000 came from:\n%s", set.Record())
	}
	t.Logf("unset: %s", fromCatalog.Record())
	t.Logf("set:   %s", set.Record())

	t.Setenv(recall.CeilingVariable, "a lot")
	if _, err := recall.BudgetFor("claude-opus-5"); err == nil {
		t.Fatal("a ceiling that is not a token count was accepted, and a budget nobody can read is worse than none")
	} else {
		t.Logf("refused: %v", err)
	}
}

func TestTheRecordedSessionSaysWhichWindowDecidedItsBudgetAndWhyItIsOff(t *testing.T) {
	store := session.NewStore(t.TempDir())
	for _, budget := range []recall.Budget{
		budgetFor(t, "anthropic/claude-haiku-4-5-20251001"),
		budgetFor(t, "anthropic/claude-sonnet-5"),
	} {
		id := "turn-" + strings.ReplaceAll(budget.Model, "/", "-")
		header := session.Header{
			ID:             id,
			Root:           id,
			At:             time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC),
			Model:          budget.Model,
			ContextCeiling: budget.CeilingTokens,
			ContextTarget:  budget.Bands.Target(),
			AutoCompaction: budget.Record(),
		}
		if err := store.Write(header, nil); err != nil {
			t.Fatalf("write %s: %v", id, err)
		}
		read, err := store.Header(id)
		if err != nil {
			t.Fatalf("read %s back: %v", id, err)
		}
		if read.ContextCeiling != budget.CeilingTokens || read.ContextTarget != budget.Bands.Target() {
			t.Fatalf("%s recorded ceiling %d target %d, read back %d and %d",
				budget.Model, budget.CeilingTokens, budget.Bands.Target(), read.ContextCeiling, read.ContextTarget)
		}
		if read.AutoCompaction != budget.Record() {
			t.Fatalf("%s recorded %q and read back %q", budget.Model, budget.Record(), read.AutoCompaction)
		}
		t.Logf("%s: ceiling %d, target %d, %s", budget.Model, read.ContextCeiling, read.ContextTarget, read.AutoCompaction)
	}
}

func TestTheDistilledCarrySaysWhatCameBackAndTheHandleCarryOnlySaysWhatWasCalled(t *testing.T) {
	cfg := recall.Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 256}
	conversation := recall.Conversation{Entries: []recall.Entry{
		{Step: 1, Tool: "bash", SupersedeKey: `bash {"command":"pnpm -r test"}`, Text: "1163 tests passed, vitest\n" + strings.Repeat("detail\n", 200)},
		{Step: 1, Tool: "read", SupersedeKey: `read {"path":"package.json"}`, Text: "{\"name\":\"tasks\"}"},
		{Step: 2, Text: "the tests pass"},
	}}

	names, err := recall.HandleCarry(recall.NewStore(t.TempDir()), cfg, conversation)
	if err != nil {
		t.Fatalf("handle carry: %v", err)
	}
	distilled, err := recall.DistilledCarry(recall.NewStore(t.TempDir()), cfg, conversation)
	if err != nil {
		t.Fatalf("distilled carry: %v", err)
	}

	if strings.Contains(names.Text, "1163 tests passed") {
		t.Fatal("the handle carry carries content, which is not the arm being measured against")
	}
	if !strings.Contains(distilled.Text, "1163 tests passed, vitest") {
		t.Fatalf("the distilled carry drops what the result said:\n%s", distilled.Text)
	}
	if len(names.Results) != 1 || len(distilled.Results) != 2 {
		t.Fatalf("the handle carry named %d results and the distilled one %d, want 1 and 2: a result under the store floor is still worth naming",
			len(names.Results), len(distilled.Results))
	}
	if !strings.Contains(distilled.Text, `read {"path":"package.json"}`) {
		t.Fatalf("the distilled carry drops the small result the handle carry also drops:\n%s", distilled.Text)
	}
}
