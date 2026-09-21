## What it is

`tofu` (named Boji in the docs) is a personal Go coding-agent harness. One static binary, no cgo, no daemon. The stance: **code owns the loop, a language model plans and writes, and a small typed decision model called Jev classifies state wherever other harnesses use a prose rule, a regex, or a frontier-model guess.** Not released, not sold, local-only git on `develop`.

## How it hangs together

Five roots, dependency order bottom-up:

- **`catalog/`**: everything shipped as data, no code, bottom of the import graph. Models, subscriptions, roles, question sets, policies (thresholds), house rules. Resolved in three layers, field by field: embedded, then `~/.boji/`, then `./.boji/`, last mention wins. Refusals are named by file and field, never skipped.
- **`internal/`**: 19 packages. `judge/` (jev client, question, policy, ledger, state) is the instrument; `turn/` is the agent loop with its tool set (`glob`, `grep`, `search`, `symbols`, `read`, `edit`, `bash`, `web`, `github`, `plan`, `memo`); `session/` is one append-only record; plus `llm/` (wire protocols, creds, quota), `rule/`, `sift/`, `recall/`, `shell/`, `transform/`, `konst/` (every limit, one file).
- **`cmd/tofu/`**: ~25 verbs. `run`, `judge`, `check`, `why`, `replay`, `session`, `shells`, `context`, `sift`, `label`, `catalog`, `rules`, `settings`, `doctor`, `login`, `models`, `frame`.
- **`interface/tui/`**: Charm v2 stack (`charm.land`, bubbletea 2.0.9). Five views on five digits: chat, work, file edits, sub-agents, shells. Built last, deliberately. Every event carries a `#a3f9c1` id; the views are a query over one record, not five records.
- **`bench/`**: 22 benchmark packages with dated markdown reports. This is the load-bearing part.

## The unusual bits

**Money is in the model name.** `claude-sub/claude-opus-5` vs `anthropic/claude-opus-5` are different entries because the same model reached two ways bills two ways. OpenRouter's key pays for Jev and nothing else; every LM call rides a subscription by speaking the vendor's wire protocol with a self-minted token.

**Every mechanism must have an off-arm, and the arm has to be the thing actually replaced.** The reports are honest about losing: read `bench/stopcheck/report-2026-09-21.md`, the typed arm wins aggregate agreement 91% to 87.2% but *loses* on catching real stops, 11 of 15 against 14, and the report concludes neither arm is fit to end a turn. `bench/sift` and `bench/readworth` both record the cheap arm beating the judgement and the cheap arm shipping.

**No comments anywhere**, enforced by hook and by `tofu lint`. A comment is treated as a repair for a name that failed.

**The repo is its own test subject.** `tofu_lint_comments`, `tofu_rules_check`, `tofu_judge`, `tofu_why`, `tofu_replay` are tofu's own verbs run against tofu's own tree.

## Where it is

E1 of the epic list, `~8,885` lines source / `6,419` test by the last stated count. Versioned, and there will never be a 1.0.0 by decision. The board (`.local/boji/tickets/BOARD.md`, 3,017 lines, written as prose for a human) says: one ticket in `doing/`, twenty-five in `review/`, nothing tree-wide verified as a whole.

Two recent self-corrections worth knowing, both caught by the owner and both now rules in `CLAUDE.md`: a benchmark corpus that leaked its own answers into the questions, and an agent turn compared against a single tool call. A mutation run hit 30.6 GB and is frozen until bounded.

## Caveats on the above

I read the report, changelog, catalog README, board and a handful of files; I did not read the design docs under `.local/boji/planning/` or run a build. If the "code owns the loop" framing is off in a way that matters, that's from the docs describing intent and me not having watched it run.
