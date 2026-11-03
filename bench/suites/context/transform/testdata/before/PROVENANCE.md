# Where package.json and src/index.ts came from

**Copied**, on 2026-09-19, two files, 294 bytes across this directory and `src/`. This one statement covers both.

They are the pre-edit state of the only two files the recorded turns in `../sessions` wrote to that already existed. They were copied out of `.playground/hono-codex`, and on 2026-09-23 they are still byte identical to that tree: `diff` reports no difference on either file. `transform.Load` reads them through `pristine()` to get the `before` side of every write, which is what makes a typed-edit cost computable against a recording.

**The seed itself was hand written by the owner before any arm ran**, which is what a seed is for: the same five files in `hono-claude`, `hono-codex` and `hono-boji`, verified identical by hash on 2026-09-18, so each arm starts from the same tree. `.playground/hono-claude` and `.playground/hono-boji` have since been run forward and no longer match this directory. `hono-codex` still does.

## What a reader should not conclude

**This is a Hono starter with one route.** Nine and eleven lines. Every "cost of rewriting an existing file" figure in `report-2026-09-20.md` rests on files this small, and the report says the saving is real and that this corpus has almost no instance of it: 3 of 14 writes touched an existing file at all.

## Leakage

**Not applicable, established rather than assumed.** The seed carries no question and no label. The thing measured is a token count arrived at by arithmetic over the diff between this state and the recorded write, and there is no verdict for a fixture to name. Count: zero eligible rows, zero leaks.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `bench/corpus.Scrub`: no hit.
