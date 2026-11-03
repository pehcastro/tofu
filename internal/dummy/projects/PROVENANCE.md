# projects

Small source trees that a bench copies whole into a fresh folder before an agent works in it. Each folder is copied as it is, so nothing else lives inside one.

| folder | origin | readers |
|---|---|---|
| cart | a hand-written Go module (cart, discount, inventory, audit, page, report and one test) made as the seed project for the rivals bench (f360965a, 2026-10-13) | bench/suites/agent/rivals, bench/suites/perf/profile |
| v2-before | the TypeScript seed project of the v2 harness task, see v2-before/provenance.json | bench/kit/harness, bench/suites/context/transform |

Moved here unchanged from `bench/suites/agent/rivals/testdata/seed` (TOFU-2283) and `bench/kit/harness/testdata/v2-before` (TOFU-2284). Nothing is fetched; nothing is regenerated.
