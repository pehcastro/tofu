package main

import (
	"regexp"
	"testing"
)

const (
	usageWithNothingSignedIn = `Usage                                                           ○ none signed in

  ✗ claude-sub  no subscription is signed in, so no model can answer
    → tofu login llm claude-sub
  ✗ jev         there is no openrouter key, so jev judges no tool call
    → tofu login classifier openrouter
`

	usageWithAKeyAndNothingSignedIn = `Usage                                                           ○ none signed in

  ✗ claude-sub  no subscription is signed in, so no model can answer
    → tofu login llm claude-sub
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
        "label": "claude-sub",
        "what": "no subscription is signed in, so no model can answer",
        "command": "tofu login llm claude-sub"
      }
    ]
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
	sameText(t, "a project with no key and no credential",
		bare.run(t, exitOK, "usage"), usageWithNothingSignedIn)
	sameText(t, "a project that recorded no reading", bare.run(t, exitOK, "usage", "--history"),
		"Usage history                                                    ○ none recorded\n")

	recorded := newProject(t, "recorded")
	writeFile(t, recorded.dir, ".env", "OPENROUTER_KEY=sk-or-v1-thistestwroteit\n")
	writeFile(t, recorded.home, ".tofu/quota/readings.jsonl", recordedQuotaReadings)
	sameText(t, "a project carrying its own key",
		recorded.run(t, exitOK, "usage"), usageWithAKeyAndNothingSignedIn)
	sameText(t, "the readings the project recorded",
		readingAge.ReplaceAllString(recorded.run(t, exitOK, "usage", "--history"), "  AGO"), quotaHistoryOfThreeWindows)

	printed := recorded.run(t, exitOK, "usage", "--json")
	oneEnvelope(t, printed, &struct{}{})
	sameText(t, "tofu usage --json", normalEnvelope(printed, nil), usageEnvelopeWithAKeyAndNothingSignedIn)
	printed = recorded.run(t, exitOK, "usage", "--history", "--json")
	oneEnvelope(t, printed, &struct{}{})
	sameText(t, "tofu usage --history --json", normalEnvelope(printed, nil), quotaHistoryEnvelope)
}
