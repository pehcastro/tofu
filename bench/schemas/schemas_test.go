package schemas

import (
	"strings"
	"testing"

	"tofu/internal/sys"
)

const targetTurnID = TargetTurnID

func TestFullRegistryMatchesTheDocumentedCount(t *testing.T) {
	registry := FullRegistry(t.TempDir())
	defs := registry.Definitions()
	if len(defs) != FullToolCount {
		t.Fatalf("got %d tools, want %d: %v", len(defs), FullToolCount, Names(defs))
	}
}

func TestMeasureAgreesWithTheRealWireEncoder(t *testing.T) {
	defs := FullRegistry(t.TempDir()).Definitions()
	whole := Measure(defs)
	if whole.WireBytes <= 0 {
		t.Fatalf("real wire diff was not computed: %d", whole.WireBytes)
	}
	if whole.SumBytes <= 0 || whole.SumBytes > whole.WireBytes {
		t.Fatalf("self-marshalled sum %d should be positive and at or under the wire figure %d", whole.SumBytes, whole.WireBytes)
	}
	apart := whole.WireBytes - whole.SumBytes
	if apart < 0 || apart > 200 {
		t.Fatalf("self-marshalled sum and the real wire diff should be within array-wrapper distance of each other, got %d bytes apart", apart)
	}
	if len(whole.Tools) != len(defs) {
		t.Fatalf("got %d per-tool rows, want %d", len(whole.Tools), len(defs))
	}
	for _, tc := range whole.Tools {
		if tc.Bytes <= 0 {
			t.Fatalf("tool %s measured at %d bytes", tc.Name, tc.Bytes)
		}
	}
}

func TestReadSessionsFindsTheTargetTurn(t *testing.T) {
	usable, _, err := ReadSessions(sys.RecordedStateDir("sessions"))
	if err != nil {
		t.Fatal(err)
	}
	var found *TurnUsage
	for i := range usable {
		if usable[i].ID == targetTurnID {
			found = &usable[i]
		}
	}
	if found == nil {
		t.Skip(".tofu/sessions no longer carries " + targetTurnID + ", a local log rotated it away")
	}
	if len(found.Steps) == 0 {
		t.Fatal("the target turn carries no steps")
	}
	write, ok := found.FirstCacheWrite()
	if !ok || write <= 0 {
		t.Fatalf("the target turn's first step should carry a cache write, got %d, present=%v", write, ok)
	}
	if found.SumCacheRead() <= write {
		t.Fatalf("a multi-step turn should read the first write back at least once, sum read %d vs first write %d", found.SumCacheRead(), write)
	}
	registry := FullRegistry(t.TempDir())
	registryNames := Names(registry.Definitions())
	missing, _ := Diff(found.CalledToolNames(), registryNames)
	if len(missing) != 0 {
		t.Fatalf("the target turn called tools this registry does not carry: %v", missing)
	}
}

func TestCostOfDeferringNamesTheMechanism(t *testing.T) {
	defs := FullRegistry(t.TempDir()).Definitions()
	whole := Measure(defs)
	called := []string{"bash", "edit", "glob", "project_report", "write"}
	cost := CostOfDeferring(whole, called, FullToolCount)
	if cost.DistinctToolsUsed != len(called) {
		t.Fatalf("got %d distinct tools used, want %d", cost.DistinctToolsUsed, len(called))
	}
	if cost.ExtraRoundTrips != len(called) {
		t.Fatalf("got %d extra round trips, want one per distinct tool used, %d", cost.ExtraRoundTrips, len(called))
	}
	if cost.UnusedSchemaTokens <= 0 || cost.UnusedSchemaTokens >= whole.SumTokens {
		t.Fatalf("unused schema tokens should sit strictly between 0 and the whole, got %d of %d", cost.UnusedSchemaTokens, whole.SumTokens)
	}
}

func TestBuildCohortReadsRealRecordedTurns(t *testing.T) {
	cohort, err := BuildCohort(sys.RecordedStateDir("sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if cohort.UsableTurns == 0 {
		t.Skip(".tofu/sessions carries no usable turns on this machine")
	}
	if cohort.WithCacheWrite == 0 {
		t.Fatal("no recorded turn in the cohort carried a cache-write step")
	}
	if cohort.TotalCacheRead == 0 {
		t.Fatal("the cohort read nothing back from cache, which contradicts the harness report's own figures")
	}
}

func TestReportRenders(t *testing.T) {
	usable, _, err := ReadSessions(sys.RecordedStateDir("sessions"))
	if err != nil {
		t.Fatal(err)
	}
	var target TurnUsage
	for _, turn := range usable {
		if turn.ID == targetTurnID {
			target = turn
		}
	}
	if len(target.Steps) == 0 {
		t.Skip(".tofu/sessions no longer carries " + targetTurnID)
	}
	defs := FullRegistry(t.TempDir()).Definitions()
	whole := Measure(defs)
	cohort, err := BuildCohort(sys.RecordedStateDir("sessions"))
	if err != nil {
		t.Fatal(err)
	}
	names := target.CalledToolNames()
	deferred := CostOfDeferring(whole, names, FullToolCount)
	missing, _ := Diff(names, Names(defs))
	report := Report{
		Date:                "2026-09-23",
		Machine:             "DESKTOP-AHUN9RO",
		BuildNote:           "tree at the commit `go test` ran against, tofu's own claude-sub credential, wire anthropic",
		Whole:               whole,
		ToolCount:           FullToolCount,
		Target:              target,
		TargetNames:         names,
		Cohort:              cohort,
		Deferred:            deferred,
		CalledNotInRegistry: missing,
	}
	rendered := report.Render()
	if rendered == "" {
		t.Fatal("the report rendered empty")
	}
	for _, section := range []string{"## Verdict", "## What was counted", "## Whole and per-tool schema cost"} {
		if !strings.Contains(rendered, section) {
			t.Fatalf("the report is missing the %q section", section)
		}
	}
}
