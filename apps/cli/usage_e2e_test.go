package main

import (
	"regexp"
	"testing"

	"tofu/internal/cli/output"
	testkit "tofu/internal/testkit/cli"
)

const (
	usageWithNothingSignedIn = `Usage                                                           ○ none signed in

  ✗ orchestrator  no subscription is signed in, so no model can answer
    → tofu login llm <source>
  ✗ jev           the gate is off because no classifier is bound: store its
    key with tofu login classifier and pick it in Models
    → tofu models bind classifier <provider/model>

classifier
  calls     0 in the last 24h
  spend     USD 0.00000 charged, USD 0.00000 at list price, 0 unpriced
`

	usageWithAKeyAndNothingSignedIn = `Usage                                                           ○ none signed in

  ✗ orchestrator  no subscription is signed in, so no model can answer
    → tofu login llm <source>

classifier
  calls     0 in the last 24h
  spend     USD 0.00000 charged, USD 0.00000 at list price, 0 unpriced
`

	usageEnvelopeWithAKeyAndNothingSignedIn = `{
  "tofu": "VERSION",
  "verb": "usage",
  "ok": true,
  "at": "AT",
  "data": {
    "state": "none",
    "providers": [],
    "spend_limit": "spend limit: tofu sets none, an api key's spending limit is the provider's, set on the account that issued the key",
    "missing": [
      {
        "label": "orchestrator",
        "what": "no subscription is signed in, so no model can answer",
        "command": "tofu login llm <source>"
      }
    ],
    "classifier": {
      "window": "24h",
      "calls": 0,
      "charged_usd": 0,
      "list_usd": 0,
      "unpriced_calls": 0
    }
  },
  "problems": []
}
`

	recordedQuotaReadings = `{"provider":"claude-sub","account":1,"at":"2026-09-20T08:00:00Z","windows":[{"id":"5h","used_fraction":0.42,"resets_at":"2026-09-20T13:00:00Z"},{"id":"7d","used_fraction":0.1,"resets_at":"2026-09-27T00:00:00Z"}]}
this line is not a reading and tofu has to say so rather than drop it
{"provider":"codex-sub","account":2,"at":"2026-09-21T09:30:00Z","windows":[{"id":"5h","used_fraction":0.875,"resets_at":"2026-09-21T14:30:00Z"}]}
`

	quotaHistoryOfThreeWindows = `Usage history · 3 readings                                  ⚠ 1 unreadable lines

  claude-sub  5h  ▓▓▓▓▓░░░░░░░  42%  AGO
  claude-sub  7d  ▓░░░░░░░░░░░  10%  AGO
  codex-sub   5h  ▓▓▓▓▓▓▓▓▓▓▓░  88%  AGO
`

	quotaHistoryEnvelope = `{
  "tofu": "VERSION",
  "verb": "usage --history",
  "ok": false,
  "at": "AT",
  "data": {
    "readings": [
      {
        "at": "2026-09-20T08:00:00Z",
        "provider": "claude-sub",
        "window": "5h",
        "used_fraction": 0.42
      },
      {
        "at": "2026-09-20T08:00:00Z",
        "provider": "claude-sub",
        "window": "7d",
        "used_fraction": 0.1
      },
      {
        "at": "2026-09-21T09:30:00Z",
        "provider": "codex-sub",
        "window": "5h",
        "used_fraction": 0.875
      }
    ],
    "unreadable_lines": 1
  },
  "problems": [
    {
      "what": "1 unreadable lines skipped"
    }
  ]
}
`
)

var readingAge = regexp.MustCompile(`(?m)  \d+d( \d+h)? ago$`)

func TestE2EUsageReadsTheKeyOfTheProjectAndTheQuotaHistoryOfTheHome(t *testing.T) {
	bare := newProject(t, "bare")
	testkit.SameText(t, "a project with no key and no credential",
		bare.run(t, output.ExitOK, "usage"), usageWithNothingSignedIn)
	testkit.SameText(t, "a project that recorded no reading", bare.run(t, output.ExitOK, "usage", "--history"),
		"Usage history                                                    ○ none recorded\n")

	recorded := newProject(t, "recorded")
	testkit.WriteFile(t, recorded.dir, ".env", "TYPESAFE_API_KEY=made-up-typesafe-thistestwroteit\n")
	testkit.WriteFile(t, recorded.dir, ".tofu/roles/classifier.yaml", "model: typesafe/jev-latest\n")
	testkit.WriteFile(t, recorded.home, ".tofu/quota/readings.jsonl", recordedQuotaReadings)
	testkit.SameText(t, "a project carrying its own key",
		recorded.run(t, output.ExitOK, "usage"), usageWithAKeyAndNothingSignedIn)
	testkit.SameText(t, "the readings the project recorded",
		readingAge.ReplaceAllString(recorded.run(t, output.ExitOK, "usage", "--history"), "  AGO"), quotaHistoryOfThreeWindows)

	printed := recorded.run(t, output.ExitOK, "usage", "--json")
	oneEnvelope(t, printed, &struct{}{})
	testkit.SameText(t, "tofu usage --json", normalEnvelope(printed, nil), usageEnvelopeWithAKeyAndNothingSignedIn)
	printed = recorded.run(t, output.ExitOK, "usage", "--history", "--json")
	oneEnvelope(t, printed, &struct{}{})
	testkit.SameText(t, "tofu usage --history --json", normalEnvelope(printed, nil), quotaHistoryEnvelope)
}
