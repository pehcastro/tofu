# Where empty_evidence@1.yaml came from

**Written**, on 2026-09-21, one file, 154 bytes, and written is right: it is a negative probe for the rule gate. It is a rule that parses cleanly and is still a fault, because its `evidence` field is the empty string. `qadomain_test.go:TestARuleThatParsesWithNoEvidenceIsAFault` asserts the fault list is exactly `[empty_evidence carries no evidence]`, so an extra fault or a missing one fails.

No real rule could stand in for it. `TestEveryQARuleDistilledFromASkillCitesItAndCarriesEvidence` asserts that every rule under `library/qa` has evidence, so a real rule with none would be a bug rather than a fixture.

## Leakage: the id names the fault, and the gate does not read the id

**Method, run on 2026-09-23.** `QARuleFaults` in `qadomain.go` was read to find what it consumes: `Kind`, `Source` and `Evidence`, and nothing else. The `id` is used only to build the message a person reads. **The gate could not be influenced by the name if it tried.**

**Count: 1 of 1 rows names its own answer**, in the id, the file name and the directory name, all three. That is on purpose for a probe a person has to recognise at a glance, and it is inert because no arm reads any of them. It would stop being inert the day a judged arm is asked to classify a rule from its text, since that arm gets the answer in the first field. Not repaired.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit.
