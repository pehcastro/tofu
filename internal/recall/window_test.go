package recall_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
	"tofu/internal/recall"
	"tofu/internal/session"
)

func budgetFor(t *testing.T, model string, windowTokens int) recall.Budget {
	t.Helper()
	budget, err := recall.BudgetFor(model, windowTokens)
	if err != nil {
		t.Fatalf("budget for %s: %v", model, err)
	}
	return budget
}

func TestTheBandsDoNotMoveWhenTheModelDoes(t *testing.T) {
	small := budgetFor(t, "anthropic/claude-haiku-4-5-20251001", 200000)
	if small.CeilingTokens != konst.ContextCeilingTokens {
		t.Fatalf("a 200000 token model runs at a %d token ceiling, want the %d tofu operates under",
			small.CeilingTokens, konst.ContextCeilingTokens)
	}
	for _, other := range []recall.Budget{
		budgetFor(t, "anthropic/claude-opus-5", 1000000),
		budgetFor(t, "anthropic/claude-sonnet-5", 0),
	} {
		if other.Bands != small.Bands || other.CeilingTokens != small.CeilingTokens {
			t.Fatalf("%s runs at a %d token ceiling with bands %+v and %s at %d with %+v: the ceiling is ours and nothing about our prompt shrinks on a smaller model",
				other.Model, other.CeilingTokens, other.Bands, small.Model, small.CeilingTokens, small.Bands)
		}
	}
	t.Logf("a 200000 token model, a 1000000 token one and one nobody has a number for all run under %+v", small.Bands)
}

func TestTheBandsKeepTheProportionsTheMeasurementEstablished(t *testing.T) {
	shipped := recall.ShippedBands()
	if shipped != (recall.Bands{Identity: 12000, Facts: 3000, WorkingSet: 18000, Recent: 30000}) {
		t.Fatalf("the bands at the 250000 token ceiling are %+v, and the measurements behind them are 12000, 3000, 16000, 30000 from real turns, the identity one from a 9603 token instruction and tool schema block the provider billed", shipped)
	}
	if got := recall.BandsOf(20000); got != (recall.Bands{Identity: 960, Facts: 240, WorkingSet: 1440, Recent: 2400}) {
		t.Fatalf("the ceiling a developer sets to exercise compaction gives %+v, want every band at a twelfth and a half of the shipped one", got)
	}
}

func TestAModelWithNoRecordedWindowStillCompactsAtTheOperatingCeiling(t *testing.T) {
	budget := budgetFor(t, "anthropic/claude-sonnet-5", 0)
	if !budget.Automatic {
		t.Fatal("a model with no recorded window was given a budget that acts on nothing, which makes the unknown case the expensive one")
	}
	cfg := recall.Config{BytesPerThousandTokens: 1000, CompactFloorBytes: 8}
	huge := recall.Conversation{Entries: []recall.Entry{
		{Step: 1, Tool: "read", Text: strings.Repeat("a", 4000000)},
		{Step: 2, Text: "now"},
	}}

	if !budget.Crossed(cfg, huge) {
		t.Fatal("four million bytes did not cross the budget of a model with no recorded window, so that model never compacts")
	}
	if err := budget.RefuseOverWindow(4000000); err != nil {
		t.Fatalf("an unrecorded window refused a request: %v", err)
	}
	if !strings.Contains(budget.Record(), "no context window is recorded for anthropic/claude-sonnet-5") {
		t.Fatalf("the record does not say the wall is unknown:\n%s", budget.Record())
	}
	t.Logf("%s", budget.Record())
}

func TestARequestOverTheModelsWindowIsRefusedNamingTheWindowAndTheSize(t *testing.T) {
	budget := budgetFor(t, "anthropic/claude-haiku-4-5-20251001", 200000)
	if err := budget.RefuseOverWindow(200000); err != nil {
		t.Fatalf("a request that exactly fills the window was refused: %v", err)
	}
	err := budget.RefuseOverWindow(200001)
	var over *recall.OverWindow
	if !errors.As(err, &over) {
		t.Fatalf("a request past the window came back as %v, want a typed refusal", err)
	}
	if over.WindowTokens != 200000 || over.RequestTokens != 200001 {
		t.Fatalf("the refusal carries %+v, want the window and the size the request was", over)
	}
	if !strings.Contains(err.Error(), "200000") || !strings.Contains(err.Error(), "200001") {
		t.Fatalf("the refusal does not name both numbers: %v", err)
	}
	t.Logf("refused: %v", err)
}

func TestTheRecordSaysWhereTheWindowItUsedCameFrom(t *testing.T) {
	budget := budgetFor(t, "openai/gpt-5.6-sol", 272000)
	budget.WindowSource = "as the codex account reports it under codex client version 0.55.0"
	if !strings.Contains(budget.Record(), budget.WindowSource) {
		t.Fatalf("the record does not say which window was used:\n%s", budget.Record())
	}
	t.Logf("%s", budget.Record())
}

func TestTheCeilingIsSetForOneRunAndTheRecordSaysWhereTheNumberCameFrom(t *testing.T) {
	shipped := budgetFor(t, "anthropic/claude-opus-5", 1000000)
	if shipped.CeilingTokens != konst.ContextCeilingTokens {
		t.Fatalf("with nothing set, a 1000000 token model gave a %d token ceiling", shipped.CeilingTokens)
	}

	t.Setenv(recall.CeilingVariable, "20000")
	set := budgetFor(t, "anthropic/claude-opus-5", 1000000)
	if set.CeilingTokens != 20000 || set.Bands.Target() != 5040 {
		t.Fatalf("%s=20000 gave a %d token ceiling and a %d token target, want 20000 and 5040",
			recall.CeilingVariable, set.CeilingTokens, set.Bands.Target())
	}
	if !strings.Contains(set.Source, recall.CeilingVariable) || !strings.Contains(set.Record(), "20000") {
		t.Fatalf("the record does not say where the 20000 came from:\n%s", set.Record())
	}
	t.Logf("unset: %s", shipped.Record())
	t.Logf("set:   %s", set.Record())

	t.Setenv(recall.CeilingVariable, "a lot")
	if _, err := recall.BudgetFor("anthropic/claude-opus-5", 1000000); err == nil {
		t.Fatal("a ceiling that is not a token count was accepted, and a budget nobody can read is worse than none")
	} else {
		t.Logf("refused: %v", err)
	}
}

func TestTheRecordedSessionCarriesTheCeilingItRanAtWhateverTheModelsWindowWas(t *testing.T) {
	store := session.NewStore(t.TempDir())
	for _, budget := range []recall.Budget{
		budgetFor(t, "anthropic/claude-haiku-4-5-20251001", 200000),
		budgetFor(t, "anthropic/claude-sonnet-5", 0),
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

func TestBothCarriesNameEverySourceAndTheDistilledOneSaysMoreAboutEach(t *testing.T) {
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

	for _, carry := range []recall.Carry{names, distilled} {
		if len(carry.Results) != 2 || len(carry.Facts) != 2 {
			t.Fatalf("a carry named %d results and %d facts, want both sources: a result under the store floor is still worth keeping",
				len(carry.Results), len(carry.Facts))
		}
		for _, want := range []string{"pnpm -r test", "package.json", "1163 tests passed"} {
			if !strings.Contains(carry.Text, want) {
				t.Fatalf("a carry does not name %q:\n%s", want, carry.Text)
			}
		}
	}
	if len(distilled.Text) <= len(names.Text) {
		t.Fatalf("the distilled carry is %d bytes against %d for the handle carry, and its longer signposts are the only difference",
			len(distilled.Text), len(names.Text))
	}
	t.Logf("handle carry, %d bytes:\n%s\ndistilled carry, %d bytes:\n%s", len(names.Text), names.Text, len(distilled.Text), distilled.Text)
}
