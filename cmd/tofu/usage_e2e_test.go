package main

import "testing"

const (
	usageWithNothingSignedIn = `no subscription credential is stored                      none

  claude-sub  no subscription is signed in, so no model can answer
              run tofu login claude-sub
  jev         there is no openrouter key, so jev judges no tool call
              run tofu login openrouter

  spend       tofu sets none, an api key's spending limit is the provider's,
                set on the account that issued the key
`

	usageWithAKeyAndNothingSignedIn = `no subscription credential is stored                      none

  claude-sub  no subscription is signed in, so no model can answer
              run tofu login claude-sub

  spend       tofu sets none, an api key's spending limit is the provider's,
                set on the account that issued the key
`

	recordedQuotaReadings = `{"provider":"claude-sub","account":1,"at":"2026-09-20T08:00:00Z","windows":[{"id":"5h","used_fraction":0.42,"resets_at":"2026-09-20T13:00:00Z"},{"id":"7d","used_fraction":0.1,"resets_at":"2026-09-27T00:00:00Z"}]}
this line is not a reading and tofu has to say so rather than drop it
{"provider":"codex-sub","account":2,"at":"2026-09-21T09:30:00Z","windows":[{"id":"5h","used_fraction":0.875,"resets_at":"2026-09-21T14:30:00Z"}]}
`

	quotaHistoryOfThreeWindows = `3 readings recorded
2026-09-20T08:00:00Z  claude-sub  5h  42%
2026-09-20T08:00:00Z  claude-sub  7d  10%
2026-09-21T09:30:00Z  codex-sub  5h  88%
1 unreadable lines skipped
`
)

func TestE2EUsageReadsTheKeyAndTheQuotaHistoryOfTheProjectItRunsIn(t *testing.T) {
	bare := newProject(t, "bare")
	sameText(t, "a project with no key and no credential",
		bare.run(t, exitOK, "usage"), usageWithNothingSignedIn)
	sameText(t, "a project that recorded no reading",
		bare.run(t, exitOK, "usage", "--history"), "0 readings recorded\n")

	recorded := newProject(t, "recorded")
	writeFile(t, recorded.dir, ".env", "OPENROUTER_KEY=sk-or-v1-thistestwroteit\n")
	writeFile(t, recorded.dir, ".tofu/quota/readings.jsonl", recordedQuotaReadings)
	sameText(t, "a project carrying its own key",
		recorded.run(t, exitOK, "usage"), usageWithAKeyAndNothingSignedIn)
	sameText(t, "the readings the project recorded",
		recorded.run(t, exitOK, "usage", "--history"), quotaHistoryOfThreeWindows)
}
