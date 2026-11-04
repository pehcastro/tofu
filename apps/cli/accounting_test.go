package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/agent/loop"
	"tofu/internal/models/llm/core"
	"tofu/internal/sys"
	"tofu/internal/testkit/cli"
	"tofu/internal/ui/tui/app"
)

var recordedSessionsDir = sys.RecordedStateDir("sessions")

type recordedLine struct {
	Kind string          `json:"kind"`
	Body json.RawMessage `json:"body"`
}

type recordedHeader struct {
	ID   string `json:"id"`
	Wire string `json:"wire"`
}

func readRecordedRow(t *testing.T, dir string) loop.Row {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "header.json"))
	if err != nil {
		t.Fatalf("reading the header: %v", err)
	}
	var header recordedHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		t.Fatalf("%s/header.json: %v", dir, err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "body.jsonl"))
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	row := loop.Row{ID: header.ID, Wire: header.Wire}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var entry recordedLine
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("%s/body.jsonl: %v", dir, err)
		}
		if entry.Kind != "step" {
			continue
		}
		var step loop.StepRow
		if err := json.Unmarshal(entry.Body, &step); err != nil {
			t.Fatalf("%s/body.jsonl step: %v", dir, err)
		}
		row.Steps = append(row.Steps, step)
	}
	return row
}

func recordedRowsByWire(t *testing.T) map[string][]loop.Row {
	t.Helper()
	entries, err := os.ReadDir(recordedSessionsDir)
	if err != nil {
		t.Skipf("no %s on this machine: %v", recordedSessionsDir, err)
	}
	byWire := map[string][]loop.Row{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(recordedSessionsDir, entry.Name())
		if _, err := os.Stat(filepath.Join(dir, "header.json")); err != nil {
			continue
		}
		row := readRecordedRow(t, dir)
		if len(row.Steps) > 0 {
			byWire[row.Wire] = append(byWire[row.Wire], row)
		}
	}
	return byWire
}

func TestOneCodexSessionAndOneAnthropicSessionReadBackOnTheSameScale(t *testing.T) {
	byWire := recordedRowsByWire(t)
	if len(byWire[core.WireCodex]) == 0 || len(byWire[core.WireAnthropic]) == 0 {
		t.Skipf("%s holds %d codex and %d anthropic sessions, so the two wires cannot be compared",
			recordedSessionsDir, len(byWire[core.WireCodex]), len(byWire[core.WireAnthropic]))
	}
	for _, row := range []loop.Row{byWire[core.WireCodex][0], byWire[core.WireAnthropic][0]} {
		accounting := row.PromptAccounting()
		for _, step := range row.Steps {
			fresh := accounting.FreshTokens(step.PromptTokens, step.CacheReadTokens)
			billed := accounting.BilledTokens(step.PromptTokens, step.CacheReadTokens)
			if fresh+step.CacheReadTokens != billed {
				t.Errorf("%s step %d: fresh %d plus cache read %d is not the billed %d",
					row.ID, step.Index, fresh, step.CacheReadTokens, billed)
			}
			if fresh < 0 {
				t.Errorf("%s step %d: fresh %d is negative", row.ID, step.Index, fresh)
			}
		}
		t.Logf("%s wire %s accounting %s steps %d", row.ID, row.Wire, accounting, len(row.Steps))
	}
}

type readStep struct {
	turn      string
	index     int
	billed    int
	cacheRead int
	fresh     int
}

func readStepsOf(rows []loop.Row) []readStep {
	var out []readStep
	for _, row := range rows {
		accounting := row.PromptAccounting()
		for _, step := range row.Steps {
			out = append(out, readStep{
				turn:      row.ID,
				index:     step.Index,
				billed:    accounting.BilledTokens(step.PromptTokens, step.CacheReadTokens),
				cacheRead: step.CacheReadTokens,
				fresh:     accounting.FreshTokens(step.PromptTokens, step.CacheReadTokens),
			})
		}
	}
	return out
}

func TestTwoStepsThatBillTheSameAndCacheTheSameReadBackTheSameFreshCount(t *testing.T) {
	byWire := recordedRowsByWire(t)
	codexSteps := readStepsOf(byWire[core.WireCodex])
	anthropicSteps := readStepsOf(byWire[core.WireAnthropic])
	pairs := 0
	for _, left := range codexSteps {
		for _, right := range anthropicSteps {
			if left.billed != right.billed || left.cacheRead != right.cacheRead {
				continue
			}
			pairs++
			if left.fresh != right.fresh {
				t.Errorf("codex %s step %d reads %d fresh and anthropic %s step %d reads %d fresh, both billed %d with %d cached",
					left.turn, left.index, left.fresh, right.turn, right.index, right.fresh, left.billed, left.cacheRead)
			}
		}
	}
	if pairs == 0 {
		t.Skipf("skipped 1: no codex step and anthropic step in %s share a billed total and a cache read, %d codex steps against %d anthropic steps",
			recordedSessionsDir, len(codexSteps), len(anthropicSteps))
	}
}

func TestASessionRecordedBeforeThisTicketStillReadsBack(t *testing.T) {
	byWire := recordedRowsByWire(t)
	if len(byWire[core.WireCodex]) == 0 {
		t.Skipf("no codex session in %s", recordedSessionsDir)
	}
	row := byWire[core.WireCodex][0]
	header, err := os.ReadFile(filepath.Join(recordedSessionsDir, row.ID, "header.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(header, []byte("accounting")) {
		t.Fatalf("%s was recorded after this ticket, so it proves nothing about the files already on disk", row.ID)
	}
	accounting := row.PromptAccounting()
	if accounting != core.PromptIncludesCacheReads {
		t.Fatalf("%s reads as %s, so a file recorded before this ticket is read with the wrong meaning", row.ID, accounting)
	}
	cached := 0
	for _, step := range row.Steps {
		if step.CacheReadTokens > 0 {
			cached++
			fresh := accounting.FreshTokens(step.PromptTokens, step.CacheReadTokens)
			if fresh != step.PromptTokens-step.CacheReadTokens {
				t.Errorf("%s step %d reads %d fresh from prompt %d and cache read %d",
					row.ID, step.Index, fresh, step.PromptTokens, step.CacheReadTokens)
			}
		}
	}
	if cached == 0 {
		t.Fatalf("%s has no step with a cache read, so it cannot show the old meaning", row.ID)
	}
	asRead, fresh, cacheRead := 0, 0, 0
	for _, step := range row.Steps {
		asRead += step.PromptTokens
		fresh += accounting.FreshTokens(step.PromptTokens, step.CacheReadTokens)
		cacheRead += step.CacheReadTokens
	}
	t.Logf("%s: %d of %d steps carry a cache read, the bar used to read %d in against %d fresh with %d cached",
		row.ID, cached, len(row.Steps), asRead, fresh, cacheRead)
}

type accountedModel struct {
	decision core.Decision
}

func (m accountedModel) Ask(context.Context, core.Request) (core.Decision, error) {
	return m.decision, nil
}

func TestTheBottomBarIsGivenFreshTokensOnBothWires(t *testing.T) {
	for _, each := range []struct {
		wire      string
		promptRaw int
	}{
		{core.WireCodex, 8016},
		{core.WireAnthropic, 2896},
	} {
		driver := driveApp(t)
		model := accountedModel{decision: core.Decision{
			Build:            "stub-model",
			Outcome:          core.OutcomeMessage,
			Usage:            core.Usage{InputTokens: each.promptRaw, OutputTokens: 100},
			PromptAccounting: core.PromptAccountingFor(each.wire),
			CacheReadTokens:  5120,
		}}
		stubbedTurn(cli.ScratchProject(t), model)(t.Context(), onTheSubscription, "say what you hold", driver.emit)
		stats := driver.of(app.EventStats)
		if len(stats) == 0 {
			t.Fatalf("%s: no stats event reached the bar", each.wire)
		}
		if stats[0].TokensIn != 2896 || stats[0].CacheRead != 5120 {
			t.Errorf("%s: the bar was given %d in and %d cached, want 2896 fresh and 5120 cached",
				each.wire, stats[0].TokensIn, stats[0].CacheRead)
		}
	}
}
