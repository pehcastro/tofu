package main

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/interface/tui/fixture"
	"tofu/interface/tui/frame"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/quota"
	"tofu/internal/transport"
)

func usageMoment() time.Time { return time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC) }

func usageFixture(now time.Time) usageReport {
	return usageReport{
		State:   usageServing,
		Fullest: "anthropic 7d",
		Providers: []credentialReport{
			{Provider: "anthropic", State: usageServingState, Windows: []windowReport{
				{ID: "5h", Used: 0.11, Reported: true, ResetsAt: now.Add(2*time.Hour + 6*time.Minute)},
				{ID: "7d", Used: 0.71, Reported: true, ResetsAt: now.Add(75*time.Hour + 26*time.Minute)},
				{ID: "1d"},
			}},
			{Provider: "codex", State: usageServingState, Windows: []windowReport{
				{ID: "7d", Used: 0.13, Reported: true, ResetsAt: now.Add(160*time.Hour + 15*time.Minute)},
			}},
		},
		SpendLimit: quota.SpendLimitLine(),
		ReportedAt: now,
	}
}

func TestNoQuotaWindowPrintsARawHourCount(t *testing.T) {
	now := usageMoment()
	printed := usageText(usageFixture(now), plain, now)
	for _, arithmetic := range []string{"75h", "160h"} {
		if strings.Contains(printed, arithmetic) {
			t.Fatalf("a window resets in %q, which is arithmetic rather than a duration\n%s", arithmetic, printed)
		}
	}
	for _, want := range []string{"resets in 2h 6m", "resets in 3d 3h", "resets in 6d 16h"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the report lost %q\n%s", want, printed)
		}
	}
}

func TestAWindowWithNoUsageDataDoesNotRender(t *testing.T) {
	now := usageMoment()
	printed := usageText(usageFixture(now), plain, now)
	for _, gone := range []string{" 1d ", "not reported"} {
		if strings.Contains(printed, gone) {
			t.Errorf("a window with no usage data renders %q\n%s", gone, printed)
		}
	}
	if lines := strings.Count(printed, "resets in "); lines != 3 {
		t.Fatalf("the report draws %d windows, want the 3 that reported\n%s", lines, printed)
	}
}

func TestOnlyTheFullestWindowIsColoured(t *testing.T) {
	now := usageMoment()
	painted := 0
	for _, line := range strings.Split(usageText(usageFixture(now), coloured, now), "\n") {
		if strings.Contains(line, "resets in ") && strings.Contains(line, "\x1b[") {
			painted++
		}
	}
	if painted != 1 {
		t.Fatalf("%d window rows carry an escape sequence, want only the fullest", painted)
	}
}

func TestTheQuotaColourIsAThresholdAtHalfAndFourFifths(t *testing.T) {
	for _, step := range []struct {
		fraction float64
		coloured bool
	}{{0, false}, {0.49, false}, {0.5, true}, {0.79, true}, {0.8, true}, {1, true}} {
		got := coloured.full(step.fraction, "meter")
		if (got != "meter") != step.coloured {
			t.Errorf("at %.2f the meter reads %q, coloured %v, want coloured %v",
				step.fraction, got, got != "meter", step.coloured)
		}
	}
	if coloured.full(0.5, "meter") == coloured.full(0.8, "meter") {
		t.Fatal("the warning step and the full step draw the same colour")
	}
	if plain.full(1, "meter") != "meter" {
		t.Fatal("a pipe gets colour")
	}
}

func TestBothQuotaWindowsRenderInUsageAndInTheStatusBar(t *testing.T) {
	now := usageMoment()
	var anthropicMeter, codexMeter string
	for _, line := range strings.Split(usageText(usageFixture(now), plain, now), "\n") {
		if strings.Contains(line, "71%") {
			_, after, _ := strings.Cut(line, "7d")
			anthropicMeter = strings.TrimSpace(after)
		}
		if strings.Contains(line, "13%") {
			_, after, _ := strings.Cut(line, "7d")
			codexMeter = strings.TrimSpace(after)
		}
	}
	if anthropicMeter == "" || codexMeter == "" {
		t.Fatalf("tofu usage draws no meter for one of the 7d windows: anthropic %q, codex %q", anthropicMeter, codexMeter)
	}
	quotas := quotasFrom([]pollResult{
		{report: quota.Report{Provider: quota.ClaudeSub, Windows: []quota.Window{
			{ID: "7d", Used: quota.Used{Fraction: 0.71, Reported: true}, ResetsAt: now.Add(75*time.Hour + 26*time.Minute)},
		}}},
		{report: quota.Report{Provider: quota.CodexSub, Windows: []quota.Window{
			{ID: "7d", Used: quota.Used{Fraction: 0.13, Reported: true}, ResetsAt: now.Add(160*time.Hour + 15*time.Minute)},
		}}},
	})
	if len(quotas) != 2 {
		t.Fatalf("quotasFrom returned %d quotas, want one per provider: %+v", len(quotas), quotas)
	}
	status := frame.Status{
		Context: fixture.Context(),
		At:      now,
		Quotas:  quotas,
	}
	bar := frame.Bar(status, 200)
	if !strings.Contains(bar, anthropicMeter) || !strings.Contains(bar, codexMeter) {
		t.Fatalf("the status bar does not draw both quota windows the way tofu usage does\nusage anthropic %q, codex %q\nbar %q", anthropicMeter, codexMeter, bar)
	}
}

func TestAThirdQuotaSourceAppearsByAddingDataAlone(t *testing.T) {
	now := usageMoment()
	two := quotasFrom([]pollResult{
		{report: quota.Report{Provider: quota.ClaudeSub, Windows: []quota.Window{
			{ID: "7d", Used: quota.Used{Fraction: 0.71, Reported: true}, ResetsAt: now},
		}}},
		{report: quota.Report{Provider: quota.CodexSub, Windows: []quota.Window{
			{ID: "7d", Used: quota.Used{Fraction: 0.13, Reported: true}, ResetsAt: now},
		}}},
	})
	three := quotasFrom([]pollResult{
		{report: quota.Report{Provider: quota.ClaudeSub, Windows: []quota.Window{
			{ID: "7d", Used: quota.Used{Fraction: 0.71, Reported: true}, ResetsAt: now},
		}}},
		{report: quota.Report{Provider: quota.CodexSub, Windows: []quota.Window{
			{ID: "7d", Used: quota.Used{Fraction: 0.13, Reported: true}, ResetsAt: now},
		}}},
		{report: quota.Report{Provider: "gemini", Windows: []quota.Window{
			{ID: "7d", Used: quota.Used{Fraction: 0.4, Reported: true}, ResetsAt: now},
		}}},
	})
	if len(two) != 2 || len(three) != 3 {
		t.Fatalf("adding one more report row gave %d then %d quotas, want 2 then 3", len(two), len(three))
	}
}

func isolateHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func TestUsageRefusesAnUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := usageVerb([]string{"anthropic"}, &out, &errOut, plain); code != exitUsage {
		t.Fatalf("an argument gave exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), usageFlags) {
		t.Fatalf("the error read %q", errOut.String())
	}
}

func TestUsageWithoutACredentialNamesTheFixAndWhoOwnsTheSpendLimit(t *testing.T) {
	isolateHome(t)
	var out, errOut bytes.Buffer
	if code := usageVerb(nil, &out, &errOut, plain); code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	printed := out.String()
	if !strings.HasPrefix(printed, usageNoCredential) {
		t.Fatalf("the first line does not answer the question: %q", printed)
	}
	if !strings.Contains(printed, "run tofu login "+string(cred.ClaudeSub)) {
		t.Fatalf("the output names no fix: %q", printed)
	}
	if !strings.Contains(printed, "an api key's spending limit is the provider's") {
		t.Fatalf("the spend limit line is missing from %q", printed)
	}
}

func TestUsageSeparatesASpentWindowFromABrokenCredential(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	spent := pollResult{report: quota.Report{
		Provider: quota.CodexSub,
		Windows: []quota.Window{{
			ID:       "5h",
			Used:     quota.Used{Fraction: 1, Reported: true},
			ResetsAt: now.Add(time.Hour),
		}},
	}}
	broken := pollResult{
		report: quota.Report{Provider: quota.CodexSub},
		err:    transport.Fail("cred", transport.KindAuth, nil, "the codex credential is disabled"),
	}
	spentState, brokenState := credentialState(spent, now), credentialState(broken, now)
	if !strings.Contains(spentState, "every window is spent") {
		t.Fatalf("a spent window reads %q", spentState)
	}
	if !strings.Contains(brokenState, "run tofu login "+string(cred.CodexSub)) {
		t.Fatalf("a broken credential reads %q, with no command to fix it", brokenState)
	}
	if spentState == brokenState {
		t.Fatal("a spent window and a broken credential read the same")
	}
}

func TestUsageJSONParsesAndCarriesTheSpendLimit(t *testing.T) {
	isolateHome(t)
	var out, errOut bytes.Buffer
	if code := usageVerb([]string{jsonFlag}, &out, &errOut, plain); code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	var report usageReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("tofu usage --json does not parse: %v\n%s", err, out.String())
	}
	if report.SpendLimit == "" || report.ReportedAt.IsZero() {
		t.Fatalf("the json dropped a field: %+v", report)
	}
	if len(report.Blockers) == 0 {
		t.Fatalf("a home with no credential reports no blocker: %+v", report)
	}
}

func usageSpentFixture(now time.Time) usageReport {
	spent := "every window is spent, back at " + now.Add(48*time.Hour).UTC().Format(time.RFC3339)
	return usageReport{
		State: usageAttention,
		Providers: []credentialReport{
			{Provider: "anthropic", State: spent, Windows: []windowReport{{ID: "5h"}, {ID: "7d"}}},
			{Provider: "codex", State: spent, Windows: []windowReport{{ID: "7d"}}},
		},
		SpendLimit: quota.SpendLimitLine(),
		ReportedAt: now,
	}
}

func TestUsageHeadlineDoesNotDenyACredentialItPrints(t *testing.T) {
	now := usageMoment()
	printed := usageText(usageSpentFixture(now), plain, now)
	first, _, _ := strings.Cut(printed, "\n")
	if strings.Contains(first, usageNoCredential) {
		t.Fatalf("two stored credentials are listed and the headline reads %q\n%s", first, printed)
	}
	if !strings.Contains(first, usageNoWindowReported) {
		t.Fatalf("the headline does not say what the state is: %q", first)
	}
	if count := strings.Count(printed, "every window is spent"); count != 2 {
		t.Fatalf("the body draws %d spent providers, want 2\n%s", count, printed)
	}
}

func TestUsageHeadlineSaysNothingIsStoredOnlyWhenNothingIs(t *testing.T) {
	now := usageMoment()
	empty := usageReport{State: usageNone, SpendLimit: quota.SpendLimitLine(), ReportedAt: now}
	first, _, _ := strings.Cut(usageText(empty, plain, now), "\n")
	if !strings.HasPrefix(first, usageNoCredential) {
		t.Fatalf("an empty store reads %q", first)
	}
	if got := usageHeadline(usageFixture(now)); got != "anthropic 7d is the fullest at 71%" {
		t.Fatalf("a reporting window gives the headline %q", got)
	}
}

func TestUsageJSONShapeIsUnchanged(t *testing.T) {
	var out bytes.Buffer
	if err := writeJSON(&out, usageSpentFixture(usageMoment())); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	want := `{
  "state": "needs attention",
  "providers": [
    {
      "provider": "anthropic",
      "state": "every window is spent, back at 2026-09-21T14:32:00Z",
      "windows": [
        {
          "id": "5h",
          "used_fraction": 0,
          "used_reported": false
        },
        {
          "id": "7d",
          "used_fraction": 0,
          "used_reported": false
        }
      ]
    },
    {
      "provider": "codex",
      "state": "every window is spent, back at 2026-09-21T14:32:00Z",
      "windows": [
        {
          "id": "7d",
          "used_fraction": 0,
          "used_reported": false
        }
      ]
    }
  ],
  "spend_limit": ` + strconv.Quote(quota.SpendLimitLine()) + `,
  "reported_at": "2026-09-19T14:32:00Z"
}
`
	if out.String() != want {
		t.Fatalf("the json changed shape\ngot\n%s\nwant\n%s", out.String(), want)
	}
}
