package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
	"tofu/internal/turn"
)

const (
	recordedBeforeOccupancy = "turn-18d6d2c8a10c7b60"
	recordedBeforeCaps      = "turn-18d6de33a2c09dbc"
	recordedWithCaps        = "turn-18d6d5c0ca9500a1"
)

var recordedCaps = recall.Bands{Identity: 2000, Facts: 0, WorkingSet: 9000, Recent: 20000}

func recordedSessions(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sessions := filepath.Join(dir, ".tofu", "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("make the session directory: %v", err)
	}
	if err := os.CopyFS(sessions, os.DirFS(filepath.Join("testdata", "context", "sessions"))); err != nil {
		t.Fatalf("copy the recorded sessions: %v", err)
	}
	t.Chdir(dir)
	return sessions
}

func tornSession(t *testing.T, sessions, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(sessions, id+".json"), []byte(`{"Outcome": 99}`), 0o644); err != nil {
		t.Fatalf("plant %s: %v", id, err)
	}
}

func contextRun(t *testing.T, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := run(append([]string{"context"}, args...), strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("tofu context %v exited %d: %s", args, code, errOut.String())
	}
	return out.String()
}

func TestContextOutsideAProjectNamesTheAbsenceRatherThanFailingOnAPath(t *testing.T) {
	t.Chdir(t.TempDir())
	printed := contextRun(t)
	if printed != contextNoSession+"\n" {
		t.Fatalf("outside a project tofu context printed:\n%s", printed)
	}
	var report contextReport
	if err := json.Unmarshal([]byte(contextRun(t, "--json")), &report); err != nil {
		t.Fatalf("outside a project the json does not parse: %v", err)
	}
	if report.Unmeasured != contextNoSession || report.Occupancy != nil {
		t.Fatalf("outside a project the json reads %+v", report)
	}
}

func TestContextOnASessionRecordedBeforeTheOccupancyNamesTheAbsenceAndTheFork(t *testing.T) {
	recordedSessions(t)
	printed := contextRun(t, recordedBeforeOccupancy)
	if !strings.Contains(printed, contextNeverMeasured) {
		t.Fatalf("a session recorded before the occupancy printed:\n%s", printed)
	}
	if strings.Contains(printed, "band          tokens") {
		t.Fatalf("a session that measured nothing printed a band table:\n%s", printed)
	}
	for _, want := range []string{"forked into " + recordedBeforeOccupancy + "-f2", "at 2490 tokens", "began at 622"} {
		if !strings.Contains(printed, want) {
			t.Fatalf("the fork line does not carry %q:\n%s", want, printed)
		}
	}
	t.Logf("%s", printed)
}

func TestContextOnASessionRecordedAfterItPrintsEveryBandAgainstThisBuildsCaps(t *testing.T) {
	recordedSessions(t)
	printed := contextRun(t, recordedBeforeCaps)
	if strings.Contains(printed, contextNeverMeasured) {
		t.Fatalf("a measured session printed the absence:\n%s", printed)
	}
	shipped := recall.ShippedBands()
	row := func(band string, tokens, cap int) string {
		return fmt.Sprintf("%-11s %8d  %8d   %3d%%", band, tokens, cap, recall.FillPercent(tokens, cap))
	}
	for _, want := range []string{
		row("identity", 333, shipped.Identity),
		row("facts", 0, shipped.Facts),
		row("working set", 0, shipped.WorkingSet),
		row("recent", 138, shipped.Recent),
		row("total", 471, shipped.Target()),
		row("ceiling", 471, konst.ContextCeilingTokens),
		fmt.Sprintf("%s 50000, so the caps above are this build's, totalling %d", contextCapsUnrecorded, shipped.Target()),
		"2310 bytes per thousand tokens",
	} {
		if !strings.Contains(printed, want) {
			t.Fatalf("the band table does not carry %q:\n%s", want, printed)
		}
	}
	t.Logf("%s", printed)
}

func TestContextJSONCarriesOneFieldPerBandWithTheTotalAndTheMark(t *testing.T) {
	recordedSessions(t)
	var report contextReport
	if err := json.Unmarshal([]byte(contextRun(t, recordedBeforeCaps, "--json")), &report); err != nil {
		t.Fatalf("the json does not parse: %v", err)
	}
	if report.Occupancy == nil {
		t.Fatalf("the json of a measured session carries no occupancy: %+v", report)
	}
	if report.Unmeasured != "" {
		t.Fatalf("a measured session reports itself unmeasured: %q", report.Unmeasured)
	}
	shipped := recall.ShippedBands()
	measured := *report.Occupancy
	identity := contextBandReport{Tokens: 333, Cap: shipped.Identity, Percent: recall.FillPercent(333, shipped.Identity)}
	if measured.Identity != identity {
		t.Fatalf("identity reads %+v, want %+v", measured.Identity, identity)
	}
	if measured.Facts != (contextBandReport{Cap: shipped.Facts}) {
		t.Fatalf("the facts band, which this session never filled, reads %+v", measured.Facts)
	}
	if measured.Total != 471 || measured.Mark != 50000 {
		t.Fatalf("total %d against mark %d", measured.Total, measured.Mark)
	}
	if measured.CapsRecorded {
		t.Fatalf("a step that recorded no caps reports itself as carrying them: %+v", measured)
	}
	if report.Ceiling != 250000 || report.BytesPerThousandTokens != 2310 {
		t.Fatalf("ceiling %d, bytes per thousand tokens %d", report.Ceiling, report.BytesPerThousandTokens)
	}

	var unmeasured contextReport
	if err := json.Unmarshal([]byte(contextRun(t, recordedBeforeOccupancy, "--json")), &unmeasured); err != nil {
		t.Fatalf("the json of an unmeasured session does not parse: %v", err)
	}
	if unmeasured.Occupancy != nil || unmeasured.Unmeasured != contextNeverMeasured {
		t.Fatalf("a session that measured nothing reads %+v", unmeasured)
	}
	if unmeasured.Fork == nil || unmeasured.Fork.Counts == nil {
		t.Fatalf("the fork is missing from %+v", unmeasured)
	}
	if *unmeasured.Fork.Counts != (contextForkCounts{TokensBefore: 2490, TokensAfter: 622}) {
		t.Fatalf("the fork counts read %+v", *unmeasured.Fork.Counts)
	}
}

func TestContextWithNoIdReadsTheNewestSession(t *testing.T) {
	recordedSessions(t)
	if printed := contextRun(t); !strings.Contains(printed, "session "+recordedBeforeCaps+",") {
		t.Fatalf("tofu context with no id printed:\n%s", printed)
	}
}

func TestContextReadsTheNewestReadableSessionAndSaysWhatItSteppedOver(t *testing.T) {
	tornSession(t, recordedSessions(t), "turn-torn")

	printed := contextRun(t)
	if !strings.Contains(printed, "session "+recordedBeforeCaps+",") {
		t.Fatalf("tofu context did not fall through to the newest readable session:\n%s", printed)
	}
	for _, want := range []string{"stepped over 1 session that could not be read: turn-torn", "run tofu context turn-torn to see why"} {
		if !strings.Contains(printed, want) {
			t.Fatalf("the skip notice does not carry %q:\n%s", want, printed)
		}
	}

	var report contextReport
	if err := json.Unmarshal([]byte(contextRun(t, "--json")), &report); err != nil {
		t.Fatalf("the json does not parse: %v", err)
	}
	if len(report.Skipped) != 1 || report.Skipped[0].Session != "turn-torn" {
		t.Fatalf("the json records the skips as %+v", report.Skipped)
	}
	if !strings.Contains(report.Skipped[0].Reason, "turn-torn") {
		t.Fatalf("the reason does not name the row: %q", report.Skipped[0].Reason)
	}
	t.Logf("%s", printed)
}

func TestContextWhereEverySessionIsUnreadableSaysSoRatherThanReportingAnAbsence(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, ".tofu", "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("make the session directory: %v", err)
	}
	t.Chdir(dir)
	tornSession(t, sessions, "turn-torn")

	printed := contextRun(t)
	if strings.Contains(printed, contextNoSession) {
		t.Fatalf("a directory holding a session that cannot be read reported an absence:\n%s", printed)
	}
	for _, want := range []string{contextNoneReadable, "stepped over 1 session that could not be read: turn-torn"} {
		if !strings.Contains(printed, want) {
			t.Fatalf("the output does not carry %q:\n%s", want, printed)
		}
	}
	t.Logf("%s", printed)
}

func TestContextPrintsTheCapsTheStepRecordedRatherThanThisBuilds(t *testing.T) {
	recordedSessions(t)
	printed := contextRun(t, recordedWithCaps)
	for _, want := range []string{
		"identity         500      2000    25%",
		"working set      900      9000    10%",
		"recent          4000     20000    20%",
		"total           5400     31000    17%",
		contextCapsRecorded + " 31000",
	} {
		if !strings.Contains(printed, want) {
			t.Fatalf("the output does not carry %q:\n%s", want, printed)
		}
	}
	if strings.Contains(printed, contextCapsUnrecorded) {
		t.Fatalf("a step carrying caps borrowed this build's:\n%s", printed)
	}

	var report contextReport
	if err := json.Unmarshal([]byte(contextRun(t, recordedWithCaps, "--json")), &report); err != nil {
		t.Fatalf("the json does not parse: %v", err)
	}
	if report.Occupancy == nil || !report.Occupancy.CapsRecorded {
		t.Fatalf("the json does not say the caps were recorded: %+v", report)
	}
	if report.Occupancy.Identity.Cap != recordedCaps.Identity || report.Occupancy.Recent.Cap != recordedCaps.Recent {
		t.Fatalf("the json reports caps %+v", *report.Occupancy)
	}
	t.Logf("%s", printed)
}

func TestMovingABandCapDoesNotChangeHowAStepThatRecordedItsCapsRenders(t *testing.T) {
	recordedSessions(t)
	store, err := session.Open()
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	shipped := recall.ShippedBands()
	moved := recall.Bands{Identity: 9000, Facts: 3000, WorkingSet: 1000, Recent: 77000}

	rendered := func(id string, build recall.Bands) string {
		t.Helper()
		report, err := contextOf(store, id, build)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return contextText(report)
	}

	under, moved2 := rendered(recordedWithCaps, shipped), rendered(recordedWithCaps, moved)
	if under != moved2 {
		t.Fatalf("a step that recorded its caps rendered differently once the build's caps moved:\n%s\nagainst\n%s", under, moved2)
	}
	if !strings.Contains(under, contextCapsRecorded+" 31000") {
		t.Fatalf("the recorded caps are not the ones printed:\n%s", under)
	}

	before, movedBefore := rendered(recordedBeforeCaps, shipped), rendered(recordedBeforeCaps, moved)
	if before == movedBefore {
		t.Fatal("a step that recorded no caps rendered identically under two cap sets, so the fallback is not reading the build's caps")
	}
	if !strings.Contains(movedBefore, contextCapsUnrecorded+" 50000, so the caps above are this build's, totalling 90000") {
		t.Fatalf("the borrowed caps are not named as this build's:\n%s", movedBefore)
	}
	t.Logf("recorded caps hold at 31000 under two builds; the step with none moved from %d to %d", shipped.Target(), moved.Target())
}

type onlyToolThenDone struct{ calls int }

func (m *onlyToolThenDone) Ask(context.Context, llm.Request) (llm.Decision, error) {
	m.calls++
	if m.calls > 1 {
		return llm.Decision{Build: "stub", Outcome: llm.OutcomeMessage, Content: "done"}, nil
	}
	return llm.Decision{Build: "stub", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
		{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a.md"}`)},
	}}, nil
}

func TestASessionRecordedNowNamesTheCapsItsStepsWereMeasuredAgainst(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".tofu", "sessions"), 0o755); err != nil {
		t.Fatalf("make the session directory: %v", err)
	}
	t.Chdir(dir)
	store, err := session.Open()
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	bands := recall.Bands{Identity: 1500, Facts: 0, WorkingSet: 4000, Recent: 9000}
	row, err := turn.Run(context.Background(), turn.Config{
		Model:          &onlyToolThenDone{},
		Spend:          turn.SpendAPIKey,
		Task:           "count the rules",
		ResultBytesCap: 4096,
		ArtifactDir:    filepath.Join(dir, "artifacts"),
		Budget:         recall.Budget{CeilingTokens: 14500, Bands: bands, Automatic: true, Source: "the caps this test runs under"},
		Sessions:       store,
	})
	if err != nil {
		t.Fatalf("run a turn into the scratch directory: %v", err)
	}

	printed := contextRun(t, row.ID)
	if !strings.Contains(printed, contextCapsRecorded+" 14500") {
		t.Fatalf("a session recorded now does not name its caps:\n%s", printed)
	}
	for _, want := range []string{"      1500", "      4000", "      9000"} {
		if !strings.Contains(printed, want) {
			t.Fatalf("the band table does not carry the cap %q:\n%s", strings.TrimSpace(want), printed)
		}
	}
	t.Logf("%s", printed)
}

func TestContextNamingAnUnreadableSessionSaysWhyRatherThanReportingAnAbsence(t *testing.T) {
	tornSession(t, recordedSessions(t), "turn-torn")

	var out, errOut bytes.Buffer
	code := run([]string{"context", "turn-torn"}, strings.NewReader(""), &out, &errOut)
	if code == exitOK {
		t.Fatalf("tofu context on an unreadable session exited %d and printed:\n%s", code, out.String())
	}
	said := errOut.String()
	if !strings.Contains(said, "turn-torn") || strings.Contains(said, "neither shape") {
		t.Fatalf("tofu context on an unreadable session said %q", said)
	}
	t.Logf("exit %d: %s", code, said)
}
