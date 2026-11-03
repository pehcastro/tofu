# Where tool_gate_hazards came from

**Written**, on 2026-10-24, TOFU-1391, in commit `a245b3fd`, and written is right for them: they are the arm under test, not a measurement. `tool_gate_hazards@1.yaml` is a decomposed question set of eight `noul` questions (`deletes_outside_project`, `force_push`, `sends_local_data`, `reads_credential`, `installs_package`, `writes_outside_brief`, `user_requested`, `from_untrusted`), and `tool_gate_hazards.rule.json` is the rule that turns their answers into a verdict. `bench/cost/hazard.go` loads both and scores them against the shipped `tool_gate@3` and the regex arm.

The thresholds were chosen before any answer existed, and the rule file says so in its own `chosen` field: 0.5 is the `noul` midpoint, 0.85 is `user_requested_relax_at` in `library/general/rules/tool_gate@3.yaml`. **No number in these files is a measurement.** The measurement they produce is recorded in `../answers/hazard-2026-10-24.jsonl`, whose provenance is in `../answers/PROVENANCE.md`.

Two files, 5,369 bytes: `tool_gate_hazards@1.yaml` 5,016, `tool_gate_hazards.rule.json` 353. The compile probes `spendsum/` and `spendapart/` carry their own `PROVENANCE.md`.

## Leakage

**Checked, not assumed.** The cases these questions answer are `bench/corpus/gate/cases.jsonl`, labelled `proceed` (166) or `block` (12). Method, run on 2026-10-28: each label value searched as a whole word, case folded, in both files. **Count: zero.** The word `ask` appears on four lines, always as a verb ("did not ask for", "ask the agent"), and no case is labelled `ask`.

The questions name kinds of action (a force push, a curl piped into a shell), never a case id, a command copied from a case, or a path from one.

## What it does not carry

Scanned on 2026-10-28 by `bench/corpus.TestEveryRecordedFixtureInTheTreeCarriesNothingOffTheRecordingMachine`, which runs every identity and credential pattern in `bench/corpus.Scrub`: no hit.
