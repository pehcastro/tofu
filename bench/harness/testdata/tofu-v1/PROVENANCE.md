# Where session.json and 2026-09-19.jsonl came from

Both are one real tofu run on the hono v1 task, taken out of `.tofu/sessions` and `.tofu/log` on 2026-09-23 for TOFU-437. Nothing under `.tofu` was changed, moved or deleted to make them.

Until this pair existed, `MeasureTofu` had never read a recorded transcript: the only thing in `bench/harness/testdata/boji-1` is a ledger written by a generator, and the test that measures a tofu run skipped.

## The run

`turn-18d69bfed2bf52a0`, started 2026-09-19 at 00:43:40 local. Model `claude-opus-5` on a subscription, so the run cost no money and `total_cost_usd` is 0. Outcome `stopped`, 13 steps, 99,906 ms wall clock, 13 tool calls: one read, five writes, seven shell commands, one of which failed.

Its `task` is the v1 prompt, byte for byte the file already tracked at `bench/harness/task/v1.prompt.txt`, so the fixture reveals nothing the repository did not already carry.

The 91 ledger rows in `2026-09-19.jsonl` are every judge decision that names `turn_id` `turn-18d69bfed2bf52a0`: 13 `tool_gate` and 78 `stop_check`, all in shadow mode, build `typesafe/jev-1.13-20260917`, $0.001682 in total. 65 of them are replays of an earlier decision and carry zero cost and zero latency, which is why the sum is not 91 times a per call price.

The turn's own `decision_ids` list 13 ids and every one of them is in this file.

## How they were taken

A generator under `bench/harness`, run once and deleted, read the session file whole, passed its text through `bench/corpus.Scrub`, and wrote the result. It then read `.tofu/log/2026-09-19.jsonl` line by line, kept every row whose `turn_id` is this turn's, dropped one key from each, scrubbed and wrote them.

## What was removed

**`request_id`, from all 91 ledger rows.** It is the OpenRouter generation id for a call billed to the owner's account. It is not a credential and nothing in `bench/` reads it off a recorded row, so it is dropped rather than masked: a field that is not written cannot leak.

**The absolute path in `reason.mode_reason`.** Every row names the policy file that put it in shadow mode, by the full path it had on the machine that ran it. `corpus.Scrub` rewrites the drive and the top directory, so the 13 `tool_gate` rows now read `R:\work\ephem-sh\bob\catalog\policy\tool_gate@1.yaml`. The 78 `stop_check` rows already carried a relative path and were not touched.

Nothing else changed. `session.json` is byte for byte identical to the file in `.tofu/sessions`, because `corpus.Scrub` found nothing in it to rewrite: every path the run touched is relative to the playground tree it was confined to, and no step, tool argument or assistant reply names a home directory, an account, a key or a token. That is a finding rather than luck, and `scrub_test.go` is what keeps it true.

## What was kept, and why each is safe

- `id`, `at`, `schema`, `outcome`, `wall_clock_ms`, `total_cost_usd`: the measurement itself. `id` is a local counter in hex, tied to no account.
- `task`: the v1 prompt, already tracked in this repository.
- `model` and `spend`: `claude-opus-5` and `subscription`, which is what makes the dollars column say the money is not comparable.
- `steps`, with every tool call's arguments and every assistant reply: all of it is the hono task, relative paths inside the arm's own tree, and the TypeScript it wrote.
- `decision_ids`: local ledger ids.
- On a ledger row: `answers`, `cost`, `latency_ms`, `build`, `model`, `point`, `policy`, `questions`, `verdict`, `version`, `state_builder`, `state_hash`, `turn_id`, `replay_of`. `state_hash` is a digest and no row carries a state body.

## What guards it

`scrub_test.go` reads every file in this directory and fails on anything `corpus.LeaksIn` recognises, on a home directory path, on a JSON `account` or `user_id` key with a value, and on any run of 24 or more word characters mixing upper case, lower case and a digit, which is the shape of a key and the shape nothing legitimate in this fixture has. The same test plants one of each kind and fails if the check does not fire, so a check that stops working is caught rather than quietly passing.

## What it cannot say

The turn row names no `wire`, because the recorder did not write that field in September 2026. Whether a prompt token count already includes cache reads depends on the wire, so `MeasureTofu` adds the column the anthropic way and says so in a gap on every row built from this fixture. Every step here reports zero cache reads and zero cache writes, so nothing in this fixture can tell the two accountings apart; `turn-18d6a5df2caeac68` under `testdata/recorded-session` is the one that can, and it is what `TestBilledInputCountsCacheTheWayTheClaudeArmCountsIt` uses.

The tree this run produced is not here. `TestMeasureTofuFillsTheRowFromAStoredTurnRowAndLedger` scores the gates and the checklist against a freshly staged v1 seed committed to its own git repository, so the checklist count is real and the checklist score is not. Only the turn row half and the ledger half of that row mean anything.
