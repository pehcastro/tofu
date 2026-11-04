# Map

Go code lives in four roots: `apps/` for the programs a person runs; `internal/` for the packages they share, grouped as `internal/<area>/<part>/`; `library/` for data embedded in the binary; `bench/` for measurements, one folder per suite under `bench/suites/<area>/`; everything else at the root is docs, scripts and vendored modules.

## Top folders

| Folder | Holds | Never holds |
|---|---|---|
| `apps/` | every program a person runs: `cli/` is the `tofu` binary and its verbs, `desk/` is the desktop app in Rust and gpui | code another app or package imports |
| `internal/` | every Go package the apps share, one folder per area: agent, tools, models, judge, librarian, store, project, host, cli, ui, platform, sys, konst, testkit, dummy | a `main` package, data a person edits |
| `library/` | data a person can change, embedded in the binary: rules, references, agents, skills, questions, models, subscriptions, decisions, in-app docs, changelog | logic beyond the embed and the tests that read it |
| `bench/` | suites by area in `suites/`, the dispatcher in `cmd/`, shared data in `corpus/`, libraries in `kit/`, build gates and parity in `gates/`; the contract is `bench/README.md` | product code, or anything another root imports or needs to run |
| `docs/` | the public documentation site, built with Petit | the in-app pages, which are `library/docs/` |
| `third_party/` | forked Go modules wired by `replace` in `go.mod` | tofu code |
| `scripts/` | the install scripts | build or check steps the binary runs |
| `media/` | logos and icons | screenshots for docs, which are `docs/media/` |
| `.github/` | the check and release workflows | local hooks |
| `.githooks/` | the git hooks, wired with `core.hooksPath` | agent hooks |

Not in git: `.local/`, `.tofu/`, `.claude/`, `.agents/`, `.playground/`, `.zed/`, `rtk/` and every `*.exe`.

## Where a new thing goes

| A new... | Goes in | Copy the shape of |
|---|---|---|
| verb | `apps/cli/verbs/<verb>/`, registered in `apps/cli/main.go` | `apps/cli/verbs/shells/` |
| verb output or page layout | `apps/cli/output/`, `apps/cli/page/` | `apps/cli/page/` |
| run option, wire or classifier a verb builds | `apps/cli/wiring/` | `apps/cli/wiring/` |
| verb test helper | `internal/testkit/cli/` | `internal/testkit/cli/` |
| tool an agent calls | `internal/tools/registry/<name>.go`, its engine in `internal/tools/<category>/` | `internal/tools/web/` |
| wire protocol for a model | `internal/models/llm/wire/<name>/` | `internal/models/llm/wire/anthropic/` |
| classifier provider | `internal/models/classifiers/providers/<name>/` | `internal/models/classifiers/providers/typesafe/` |
| decision point | `internal/judge/state/` | `internal/judge/state/` |
| decision question | `library/questions/<name>@1.yaml` | `library/questions/` |
| rule for a language | `library/dev/<language>/rules/` | `library/dev/go/rules/` |
| skill | `library/dev/skills/` | `library/dev/skills/` |
| slash command | `internal/agent/commands/` | `internal/agent/commands/` |
| interface panel | `internal/ui/tui/panels/<name>/` | `internal/ui/tui/panels/shells/` |
| test data two package trees share | `internal/dummy/<category>/`, read through `dummy.Path` | `internal/dummy/turns/` |
| limit or constant | `internal/konst/` | `internal/konst/` |
| operating system call | `internal/sys/`, one file per OS | `internal/sys/` |
| bench suite | `bench/suites/<area>/<suite>/`, area one of judge, rules, context, tools, agent, perf | `bench/suites/tools/search/` |
| parity case | `bench/gates/parity/cases/<area>.cases` | `bench/gates/parity/cases/` |
| public docs page | `docs/<section>/` | `docs/tools/` |

## When two folders both seem to fit

- `apps/cli/` or `internal/cli/`: a verb is a package in `apps/cli/verbs/` (boardy, shells, judge, lint, guard, hooks, settings, memory); the verbs tied to the run and the session (run, session, drive, status, models, rules, learn, memory tree, doctor, library, browser) live in `apps/cli/` itself; how a verb builds its run options, wires, classifier and names is in `apps/cli/wiring/`; what verbs print through is in `apps/cli/output/`; `internal/cli/` holds only `render/`, which `internal/ui` imports too.
- `internal/tools/registry/` or `internal/tools/<category>/`: what the model sees and calls is in `registry/`; the code that does the work is in the category.
- `internal/judge/` or `internal/models/classifiers/`: the decision and its question are in `judge/`; the client that asks the classifier is in `classifiers/`.
- `library/` or `internal/librarian/`: a file a person edits is in `library/`; the code that loads it and picks what reaches a prompt is in `internal/librarian/`.
- `internal/sys/` or `internal/platform/`: new operating system code goes in `sys/`; `platform/` holds only `credential/` and `recover/` for now.
- `docs/` or `library/docs/`: both. A feature gets a public page in `docs/` and an in-app page in `library/docs/`.
- `CHANGELOG.md` or `library/changelog/`: both, byte for byte; a test compares them.
- `bench/` or a `_test.go`: a number that is measured and compared goes in `bench/suites/<area>/`, following `bench/README.md`.

## Import rules

`tofu doctor --imports` checks these, from `apps/cli/verbs/doctor/imports.go`, and also fails when a rule matches no package:

- nothing under `apps/` or `internal/` imports `bench/`
- nothing under `internal/` imports `apps/`
- nothing under `apps/` imports `internal/dummy` outside a `_test.go` file
- nothing under `apps/cli/wiring` imports `apps/cli/verbs`
- each package under `apps/cli/verbs` imports no other one under it
- `library/` imports no tofu package
- `internal/sys`, `internal/konst` and `internal/platform/recover` import only each other
- under `internal/`, a package in `internal/judge` imports only `internal/judge`, those three, `internal/models/transport`, `internal/models/classifiers/core` and `internal/models/classifiers/providers`

## The tree

Drawn from git: three levels, four for `bench/` with each area's suites on its row, two for `third_party/`, no `testdata/` folders, and not the inside of `apps/desk/`.

```
tofu/
├── .githooks/
├── .github/
│   └── workflows/
├── apps/
│   ├── cli/
│   │   ├── output/
│   │   ├── page/
│   │   ├── sample/
│   │   ├── verbs/
│   │   └── wiring/
│   └── desk/
├── bench/
│   ├── README.md
│   ├── cmd/
│   ├── corpus/
│   ├── gates/
│   │   └── flows/  parity/  treewide/
│   ├── kit/
│   │   └── api/  harness/  report/
│   ├── stopcheck/
│   └── suites/
│       ├── agent/    drain/  fanin/  longsession/  orchestrator/  phase/  rivals/  sessiontime/  thread/  turn/
│       ├── context/  cache/  forkcache/  learn/  linenumbers/  memory/  memtree/  prefix/  recall/  schemas/  thrift/  tokencount/  tokens/  toolerrors/  transform/
│       ├── judge/    ask/  calibration/  cost/  instruction/  judge/  picker/  promote/  readworth/  sift/  wording/
│       ├── perf/     boardy/  cli/  minimal/  profile/  scale/  serve/  startup/  tui/
│       ├── rules/    enforce/  mutate/  planspawn/  reach/  review/  rules/  skills/  testquality/
│       └── tools/    browser/  nativetools/  search/  searchlarge/  shortlist/  sweep/  symbols/  tools/  websift/  wrongpath/
├── docs/
│   ├── automation/
│   ├── classifiers/
│   ├── cli/
│   ├── features/
│   ├── library/
│   ├── llms/
│   ├── media/
│   ├── start/
│   └── tools/
├── internal/
│   ├── agent/
│   │   ├── commands/
│   │   ├── context/
│   │   ├── cron/
│   │   ├── hooks/
│   │   ├── loop/
│   │   ├── prompt/
│   │   └── subagents/
│   ├── cli/
│   │   └── render/
│   ├── dummy/
│   │   ├── browser/
│   │   ├── projects/
│   │   ├── toolgate/
│   │   └── turns/
│   ├── host/
│   ├── judge/
│   │   ├── gate/
│   │   ├── ledger/
│   │   ├── method/
│   │   ├── questions/
│   │   └── state/
│   ├── konst/
│   ├── librarian/
│   │   ├── rules/
│   │   └── skills/
│   ├── models/
│   │   ├── classifiers/
│   │   ├── llm/
│   │   └── transport/
│   ├── platform/
│   │   ├── credential/
│   │   └── recover/
│   ├── project/
│   │   ├── boardy/
│   │   └── guard/
│   ├── store/
│   │   ├── learn/
│   │   ├── memory/
│   │   ├── session/
│   │   ├── settings/
│   │   └── snapshot/
│   ├── stat/
│   ├── sys/
│   ├── testkit/
│   │   ├── cli/
│   │   └── golden/
│   ├── tools/
│   │   ├── code/
│   │   ├── contract/
│   │   ├── files/
│   │   ├── registry/
│   │   ├── shell/
│   │   └── web/
│   └── ui/
│       ├── keymap/
│       ├── statusline/
│       ├── toolcard/
│       └── tui/
├── library/
│   ├── boardy/
│   │   └── hooks/
│   ├── changelog/
│   ├── decisions/
│   ├── dev/
│   │   ├── agents/
│   │   ├── frontend/
│   │   ├── go/
│   │   ├── gpui/
│   │   ├── python/
│   │   ├── react/
│   │   ├── references/
│   │   ├── rules/
│   │   ├── rust/
│   │   ├── skills/
│   │   ├── svelte/
│   │   ├── typescript/
│   │   └── vue/
│   ├── docs/
│   ├── general/
│   │   ├── agents/
│   │   ├── references/
│   │   └── rules/
│   ├── memory/
│   ├── models/
│   │   ├── anthropic/
│   │   ├── meta/
│   │   ├── openai/
│   │   ├── openrouter/
│   │   ├── prices/
│   │   ├── typesafe/
│   │   └── windows/
│   ├── qa/
│   │   ├── agents/
│   │   ├── general/
│   │   └── references/
│   ├── questions/
│   ├── subscriptions/
│   ├── tools/
│   │   └── shell/
│   └── web/
│       └── search/
├── media/
├── scripts/
└── third_party/
    ├── bluemonday/
    ├── bubbletea/
    ├── chroma/
    ├── clipboard/
    ├── gorilla-css/
    └── ultraviolet/
```
