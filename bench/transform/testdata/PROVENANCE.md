# What is under bench/transform/testdata

**No recording is here.** The turns this bench measures live once, in `bench/stopcheck/corpus`, and the tests read them from there by name. Until 2026-09-23 seventeen of them were also committed under `sessions/` in this directory, byte identical, with nothing checking that the two copies stayed so. TOFU-456 removed that copy and left the superset.

`PROVENANCE-sessions.md`, beside this file, is what that ticket left behind: which seventeen of the nineteen corpus turns the census reads, which two it leaves out and why, the leakage check, and what a reader should not conclude from the figures. It keeps its name because `bench/stopcheck/corpus/PROVENANCE.md` cites it by that name and that file is not this ticket's to edit.

`before/` is the hono v1 tree each recorded write is paired against, and it carries its own `PROVENANCE.md`.
