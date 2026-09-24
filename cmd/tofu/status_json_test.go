package main

import (
	"bytes"
	"testing"
	"time"

	"tofu/internal/golden"
)

const (
	fixtureCodexAccount = "11111111-2222-3333-4444-555555555555"
	fixtureSpentState   = "every window is spent, back at 2026-09-26T18:42:00Z"
	fixtureGate         = "no key, so the jev gate is off: " + openRouterFix
	fixtureWidestWindow = "7d:fable"
	fixtureBars         = 5
)

func fixtureMoment() time.Time { return time.Date(2026, 9, 26, 18, 42, 0, 0, time.UTC) }

func statusFixture() statusReport {
	at := fixtureMoment()
	return statusReport{
		Headline: "1 of 4 accounts need attention",
		State:    statusAttention,
		Sources: []sourceReport{
			{
				Subscription: "claude-sub",
				Accounts: []accountReport{
					{
						ID:        1,
						Account:   "first@example.test",
						Login:     "oauth, expires 2026-09-26T19:42:00Z, re-login by 2026-10-26",
						Plan:      "not reported by claude-sub",
						State:     statusInUse,
						Attention: false,
						Windows: []windowReport{
							{ID: "5h", Used: 0.25, Reported: true, ResetsAt: at.Add(3 * time.Hour)},
						},
					},
					{
						ID:        2,
						Account:   "second@example.test",
						Login:     "oauth, expires 2026-09-26T19:42:00Z, re-login by 2026-10-26",
						Plan:      "not reported by claude-sub",
						State:     statusUnchosen,
						Attention: false,
						Windows: []windowReport{
							{ID: "5h", Used: 0.62, Reported: true, ResetsAt: at.Add(90 * time.Minute)},
							{ID: "7d", Used: 0.91, Reported: true, ResetsAt: at.Add(52 * time.Hour)},
						},
					},
				},
			},
			{
				Subscription: "codex-sub",
				Accounts: []accountReport{
					{
						ID:        3,
						Account:   fixtureCodexAccount,
						Login:     "oauth, expires 2026-09-26T19:42:00Z",
						Plan:      "pro",
						State:     fixtureSpentState,
						Attention: true,
						Windows: []windowReport{
							{ID: "5h", Used: 1, Reported: true, ResetsAt: at.Add(2 * time.Hour)},
							{ID: fixtureWidestWindow, Used: 1, Reported: true, ResetsAt: at.Add(31 * time.Hour)},
						},
					},
				},
			},
			{
				Subscription: "opencode-sub",
				Accounts: []accountReport{
					{
						ID:      4,
						Account: "third@example.test",
						Login:   "oauth, expires 2026-09-26T19:42:00Z",
						Plan:    "not reported by opencode-sub",
						State:   statusInUse,
					},
				},
			},
		},
		Gate:       fixtureGate,
		ReportedAt: at,
	}
}

func TestTheStatusJSONIsUntouchedByTheLayout(t *testing.T) {
	var out bytes.Buffer
	if err := writeJSON(&out, statusFixture()); err != nil {
		t.Fatal(err)
	}
	golden.Assert(t, "status-report.json.golden", out.String())
}
