# Map

Go code lives in four roots: `apps/` for the programs a person runs; `internal/` for the packages they share, grouped as `internal/<area>/<part>/`; `library/` for data embedded in the binary; `bench/` for measurements, one folder per suite under `bench/suites/<area>/`. Everything else at the root is docs, scripts and vendored modules.

Every Go package folder under `apps/cli/`, `internal/` and `bench/` has a row below. A row whose first cell also names `gen/` or another short folder covers that sub-folder too.

## Top folders

| Folder | Holds | Never holds |
|---|---|---|
| `apps/` | every program a person runs: `apps/cli/` is the `tofu` binary and its verbs, `apps/desk/` is the desktop app in Rust and gpui | code another app or package imports |
| `internal/` | every Go package the apps share, one folder per area: agent, tools, models, judge, librarian, store, project, host, cli, ui, platform, testkit, konst, plus dummy, stat and sys | a `main` package a person runs, data a person edits |
| `library/` | data a person can change, embedded in the binary: rules, references, agents, skills, questions, models, subscriptions, decisions, in-app docs, changelog | logic beyond the embed and the tests that read it |
| `bench/` | suites by area in `bench/suites/`, the dispatcher in `bench/cmd/`, shared libraries in `bench/kit/`, build gates and parity in `bench/gates/`; the contract is `bench/README.md` | product code, or anything another root imports or needs to run |
| `docs/` | the public documentation site, built with Petit | the in-app pages, which are `library/docs/` |
| `third_party/` | forked Go modules wired by `replace` in `go.mod` | tofu code |
| `scripts/` | the install scripts | build or check steps the binary runs |
| `media/` | logos and icons | screenshots for docs, which are `docs/media/` |
| `.github/` | the check and release workflows in `.github/workflows/` | local hooks |
| `.githooks/` | the git hooks, wired with `core.hooksPath` | agent hooks |

Not in git: `.local/`, `.tofu/`, `.claude/`, `.agents/`, `.playground/`, `.zed/`, `rtk/` and every `*.exe`.

## Where a new thing goes

| A new... | Goes in | Copy the shape of |
|---|---|---|
| verb | `apps/cli/verbs/<verb>/`, registered in `apps/cli/main.go` | `apps/cli/verbs/shells/` |
| verb output or page layout | `apps/cli/output/`, `apps/cli/page/` | `apps/cli/page/` |
| run option, wire or classifier a verb builds | `apps/cli/wiring/` | `apps/cli/wiring/` |
| verb test helper | `internal/testkit/cli/` | `internal/testkit/cli/` |
| tool an agent calls | `internal/tools/<category>/<name>/` | `internal/tools/history/zoom/` |
| wire protocol for a model | `internal/models/llm/wire/<name>/` | `internal/models/llm/wire/anthropic/` |
| classifier provider | `internal/models/classifiers/providers/<name>/` | `internal/models/classifiers/providers/typesafe/` |
| decision point | `internal/judge/points/<name>/` | `internal/judge/points/memory_scope/` |
| decision question | `library/questions/<name>@1.yaml` | `library/questions/` |
| rule for a language | `library/dev/<language>/rules/` | `library/dev/go/rules/` |
| skill | `library/dev/skills/` | `library/dev/skills/` |
| slash command | `internal/agent/commands/` | `internal/agent/commands/` |
| interface panel | `internal/ui/tui/panels/<name>/` | `internal/ui/tui/panels/shells/` |
| test data two package trees share | `internal/dummy/<category>/`, read through `dummy.Path` | `internal/dummy/turns/` |
| limit or constant | `internal/konst/` | `internal/konst/` |
| operating system call | `internal/platform/<part>/`, one file per OS | `internal/platform/clipboard/` |
| bench suite | `bench/suites/<area>/<suite>/`, area one of agent, context, judge, perf, rules, tools | `bench/suites/tools/search/` |
| parity case | `bench/gates/parity/cases/<area>.cases` | `bench/gates/parity/cases/` |
| public docs page | `docs/<section>/` | `docs/tools/` |

## When two folders both seem to fit

- `apps/cli/` or `internal/cli/`: a verb is a package in `apps/cli/verbs/` when it stands alone (boardy, browser, doctor, guard, hooks, judge, learn, library, lint, login, memory, models, rules, settings, shells, status); the verbs tied to the run and the session (run, session, drive, host, setup and the rest) live in `apps/cli/` itself; how a verb builds its run options, wires, classifier and names is in `apps/cli/wiring/`; what verbs print through is in `apps/cli/output/`; `internal/cli/` holds only `internal/cli/render/`, which `internal/ui/` imports too.
- `internal/tools/registry/` or `internal/tools/<category>/`: which agent gets which tool, and the wrappers every call passes through, are in `internal/tools/registry/`; each tool is its own package in its category.
- `internal/judge/` or `internal/models/classifiers/`: the decision and its question are in `internal/judge/`; the client that asks the classifier is in `internal/models/classifiers/`.
- `library/` or `internal/librarian/`: a file a person edits is in `library/`; the code that loads it and picks what reaches a prompt is in `internal/librarian/`.
- `docs/` or `library/docs/`: both. A feature gets a public page in `docs/` and an in-app page in `library/docs/`.
- `CHANGELOG.md` or `library/changelog/`: both, byte for byte; a test compares them.
- `bench/` or a `_test.go`: a number that is measured and compared goes in `bench/suites/<area>/`, following `bench/README.md`.

## Import rules

`tofu doctor --imports` checks these, from `apps/cli/verbs/doctor/imports.go`, and also fails when a rule matches no package:

- nothing under `apps/` or `internal/` imports `bench/`
- nothing under `internal/` imports `apps/`
- nothing under `apps/` imports `internal/dummy/` outside a `_test.go` file
- nothing under `apps/cli/wiring/` imports `apps/cli/verbs/`
- each package under `apps/cli/verbs/` imports no other one under it
- `library/` imports no tofu package
- the leaves, `internal/konst/` and every package under `internal/platform/`, import only each other
- under `internal/`, a package in `internal/judge/` imports only `internal/judge/`, the leaves, `internal/models/transport/`, `internal/models/classifiers/core/` and `internal/models/classifiers/providers/`

## apps

| Folder | What lives there |
|---|---|
| `apps/cli/` | the `tofu` binary: `main.go` parses the verb, plus the verbs tied to the run and the session (run, session, drive, host, setup, compose, update, version) |
| `apps/cli/output/` | how a verb prints: text, json, refusals, the verb protocol |
| `apps/cli/page/` | the page layout and the pieces a verb's output is built from |
| `apps/cli/sample/` | sample envelopes for every verb page, used by previews and goldens |
| `apps/cli/verbs/boardy/` | `tofu boardy`: tickets, views, audit, bypass, rules |
| `apps/cli/verbs/browser/` | `tofu browser` and its profiles |
| `apps/cli/verbs/doctor/` | `tofu doctor`, including `--imports` |
| `apps/cli/verbs/guard/` | `tofu guard`, the hook checks run from the command line |
| `apps/cli/verbs/hooks/` | `tofu hooks` |
| `apps/cli/verbs/judge/` | `tofu judge`: ledger, resolve, calibrate, label, replay, sift |
| `apps/cli/verbs/learn/` | `tofu learn` |
| `apps/cli/verbs/library/` | `tofu library`, `tofu docs` and reach |
| `apps/cli/verbs/lint/` | `tofu lint` |
| `apps/cli/verbs/login/` | `tofu login`, quota history and usage |
| `apps/cli/verbs/memory/` | `tofu memory` and the memory tree |
| `apps/cli/verbs/models/` | `tofu models` |
| `apps/cli/verbs/rules/` | `tofu rules`: index, fired, check, write |
| `apps/cli/verbs/settings/` | `tofu settings` |
| `apps/cli/verbs/shells/` | `tofu shells`, scratch and cron |
| `apps/cli/verbs/status/` | `tofu status` and `tofu why` |
| `apps/cli/wiring/` | what a verb builds before it runs: accounts, registry, classifier, judge, guard, keys, names, quota, proxy |
| `apps/desk/` | the desktop app, Rust and gpui; skipped by the Go build and checks |

## internal/agent

| Folder | What lives there |
|---|---|
| `internal/agent/loop/` | the loop: turn, call, attempt, endings, config, judge, gate, ledger, advisor, inbox, history, spawn; one package until its import cycles break |
| `internal/agent/prompt/` | the system prompt a turn sends |
| `internal/agent/context/recall/` | what a turn remembers: budget, compact, elide, fork carry, window |
| `internal/agent/hooks/` | user hooks: load, fire, reply, the Claude Code hook format |
| `internal/agent/commands/` | slash commands |
| `internal/agent/cron/` | the cron engine: schedule, book, command |
| `internal/agent/subagents/` | sub-agent definitions, contracts, tiers, grants, ownership, read lists, asks and the roster |

## internal/tools

| Folder | What lives there |
|---|---|
| `internal/tools/contract/` | the Tool shape every tool implements: input, output, refusal, endings, ledger, repair |
| `internal/tools/registry/` | which agent gets which tool; memo and cite wrap every call; the verb tools |
| `internal/tools/files/read/` | the read tool |
| `internal/tools/files/write/` | the write tool |
| `internal/tools/files/edit/` `patch/` | the edit tool and its targets; `patch/` applies and diffs an edit |
| `internal/tools/files/glob/` | the glob tool |
| `internal/tools/code/search/` `engine/` | the search tool and its links; `engine/` finds, ranks and renders matches and code units |
| `internal/tools/code/symbols/` | the symbols tool, Go symbols included |
| `internal/tools/code/typecheck/` `checkers/` | the typecheck tool; `checkers/` runs tsc, vue-tsc and svelte-check from `package.json` |
| `internal/tools/code/test/` | the test tool and its runner |
| `internal/tools/shell/bash/sift/` | reads shell output and keeps what the model needs |
| `internal/tools/shell/jobs/` | the shell tool: background jobs |
| `internal/tools/shell/process/` | the process under a shell: resolve, hold, kill, log, output, console, process tree, ports |
| `internal/tools/web/` | the web tool: fetch, html, search, untrusted content |
| `internal/tools/web/github/` | the github tool |
| `internal/tools/web/browser/` | the browser tool |
| `internal/tools/web/browser/driver/` | drives Chrome: cdp, client, page, tab, frame, element, capture, install, profile |
| `internal/tools/web/browser/extension/` | the embedded browser extension |
| `internal/tools/web/browser/loop/` | the step loop that asks a classifier what to do next |
| `internal/tools/web/browser/motion/` | motion checks over captured frames |
| `internal/tools/web/browser/recipe/` | recipes and their values |
| `internal/tools/web/browser/steps/` | a scripted run of browser moves over a fixture |
| `internal/tools/history/quote/` | the quote tool |
| `internal/tools/history/recall/` | the recall tool |
| `internal/tools/history/zoom/` | the zoom tool |
| `internal/tools/human/ask/` | the tool that asks the person |
| `internal/tools/human/plan/` | the plan tool |
| `internal/tools/human/remember/` | the remember tool |
| `internal/tools/human/settings/` | the settings and rule override tools |
| `internal/tools/agents/cron/` | the cron tool |
| `internal/tools/project/board/` | the board tool |
| `internal/tools/project/ticket/` | the ticket tool |
| `internal/tools/project/report/` | the project report tool |
| `internal/tools/project/lint/` | the comment lint |
| `internal/tools/knowledge/skill/` | the skill and reference tool |
| `internal/tools/scratch/` | the scratch tool |

## internal/models

| Folder | What lives there |
|---|---|
| `internal/models/llm/core/` | the language model client: request, response, retry, stop, effort, image, idle, cut, dump, tap |
| `internal/models/llm/catalog/` | the model catalog: roles, keys, discovery, custom models |
| `internal/models/llm/catalog/data/snapshot/` | the program that snapshots the vendor catalogs |
| `internal/models/llm/accounts/` | stored accounts and logins: store, keep, login, refresh, report |
| `internal/models/llm/quota/` | quota: record, shelf, burn, pick, poll, window |
| `internal/models/llm/wire/anthropic/` | the Anthropic protocol, spoken by `claude-sub` and `anthropic` |
| `internal/models/llm/wire/codex/` | the Codex protocol |
| `internal/models/llm/wire/openai/` | the OpenAI-compatible chat protocol, used for OpenRouter |
| `internal/models/llm/wire/pins/` | the program that raises the pinned client versions |
| `internal/models/llm/providers/claude-sub/` | the Claude subscription as a source that pays, its sign-in spec and usage endpoint |
| `internal/models/llm/providers/codex-sub/` | the Codex subscription sign-in spec and usage endpoint |
| `internal/models/classifiers/` | the route from a classifier binding to its provider |
| `internal/models/classifiers/core/` | the classifier client: request, answer, validate, confidence, caps, fallback, key, record |
| `internal/models/classifiers/catalog/` | the pinned classifier builds |
| `internal/models/classifiers/providers/` | the wire every classifier provider shares |
| `internal/models/classifiers/providers/openrouter/` | Jev through OpenRouter |
| `internal/models/classifiers/providers/typesafe/` | Jev through TypeSafe |
| `internal/models/transport/` | the http client, errors and retry both kinds of model share |

## internal/judge

| Folder | What lives there |
|---|---|
| `internal/judge/questions/` | loads and lints the questions in `library/questions/` |
| `internal/judge/points/` | what every decision point shares: method, subject, version |
| `internal/judge/points/tool_gate/` | the tool gate: command scan, target, fingerprint |
| `internal/judge/points/stop_check/` | the stop check and its rule |
| `internal/judge/points/memory_scope/` | the memory scope decision |
| `internal/judge/gate/` | the pure policy: load, lock, resolve, decide, fallback |
| `internal/judge/ledger/` | the decision ledger: write, read, canon, precedent, spend, tally |

## internal/librarian, store, project, host

| Folder | What lives there |
|---|---|
| `internal/librarian/rules/` | rules: load, tree, override, trigger, and the checks a rule runs |
| `internal/librarian/skills/` | skills |
| `internal/store/session/` | sessions on disk: records, conversation, family, names, trace, usage, legacy |
| `internal/store/session/drain/` `claudecode/` | draining a finished session into memory; `claudecode/` reads Claude Code sessions |
| `internal/store/memory/entries/` | memory entries: authors, episodes, weights |
| `internal/store/memory/tree/` | the memory tree |
| `internal/store/settings/` | settings: store, accessors, search |
| `internal/store/snapshot/` | snapshots for undo |
| `internal/store/learn/` `repeat/` `words/` | learning from past sessions; `repeat/` finds repeated asks, `words/` the person's words |
| `internal/project/boardy/` | the board: tickets, epics, sprints, flow, checks, hooks, catchup |
| `internal/project/guard/` | the ownership guard behind the hooks: board, git, loc, shell, write, stamp |
| `internal/host/` | the per-project host: hub, peers, wire, serve, sessions, reports, follow, boards |

## internal/cli, ui

| Folder | What lives there |
|---|---|
| `internal/cli/render/` | text rendering verbs and the interface share: quota, weighted text |
| `internal/ui/tui/app/` | the interface root: app, keys, dialogs, commands, mouse, pickers wiring |
| `internal/ui/tui/chat/session/` | the chat view: history, groups, plans, queue, scroll |
| `internal/ui/tui/chat/feed/` | the feed of cards |
| `internal/ui/tui/chat/markdown/` | markdown rendering, streamed |
| `internal/ui/tui/chat/quote/` | quotes in chat |
| `internal/ui/tui/chat/cat/` | the cat sprite and its poses |
| `internal/ui/tui/composer/keyfield/` | the key entry field |
| `internal/ui/tui/composer/paste/` | paste and dropped files |
| `internal/ui/tui/composer/question/` | answering a question |
| `internal/ui/tui/pickers/models/` | the model picker |
| `internal/ui/tui/pickers/palette/` | the command palette and file search |
| `internal/ui/tui/pickers/settings/` | the settings picker |
| `internal/ui/tui/pickers/welcome/` | the welcome screen |
| `internal/ui/tui/panels/shells/` | the shells panel |
| `internal/ui/tui/panels/subagent/` | the sub-agent panel |
| `internal/ui/tui/panels/edits/` | the edits panel and its diff |
| `internal/ui/tui/panels/trace/` | the trace panel |
| `internal/ui/tui/panels/quota/` | the quota panel |
| `internal/ui/tui/panels/progress/` | the progress panel |
| `internal/ui/tui/panels/orders/` | the standing orders panel |
| `internal/ui/tui/frame/frame/` | the frame: top and footer |
| `internal/ui/tui/frame/frametime/` | frame timing |
| `internal/ui/tui/frame/filmstrip/` | scripted screens recorded as a filmstrip |
| `internal/ui/tui/look/look/` | colour, rows, surfaces, text, windows |
| `internal/ui/tui/look/theme/` | the theme every colour comes from |
| `internal/ui/tui/input/pointer/` | pointer, selection and tracking |
| `internal/ui/tui/input/hostkeys/` | host shortcuts and their dialog |
| `internal/ui/tui/input/links/` | links |
| `internal/ui/tui/fixture/` | interface fixtures for golden files |
| `internal/ui/keymap/` | keymaps and editor profiles |
| `internal/ui/statusline/` | the status line |
| `internal/ui/toolcard/` | tool cards: card, group, parse, shell |
| `internal/ui/toolcard/cmd/cards/` | the program that prints every tool card from a fixture |

## internal/platform and the rest

| Folder | What lives there |
|---|---|
| `internal/platform/fs/` | paths, layers, lazy values, locks, roots |
| `internal/platform/clipboard/` | the clipboard, one file per OS |
| `internal/platform/console/` | the console and virtual terminal, one file per OS |
| `internal/platform/project/` | the project root, its state folder, crash records |
| `internal/platform/scratch/` | the scratch folder and its cleanup |
| `internal/platform/credential/` | credentials and secrets |
| `internal/platform/profile/` | the owner's profile folder |
| `internal/platform/build/` | the revision, version and Go version, and where the desk build is |
| `internal/platform/safe/` | panic recovery and restart |
| `internal/testkit/cli/` | helpers for verb tests: scratch project, empty home |
| `internal/testkit/golden/` | golden files |
| `internal/konst/` | every limit and constant |
| `internal/stat/` | median, percentile, spread |
| `internal/dummy/` | test data two package trees share, read through `dummy.Path` |
| `internal/dummy/toolgate/` | tool gate cases |
| `internal/dummy/projects/cart/` | a small Go project tools run against |

## library

Data only; this section names folders and no package.

| Folder | What lives there |
|---|---|
| `library/general/rules/` `library/general/references/` `library/general/agents/` | rules, references and agents for every project |
| `library/dev/rules/` `library/dev/references/` `library/dev/agents/` `library/dev/skills/` | rules, references, agents and skills for coding |
| `library/dev/go/` `library/dev/rust/` `library/dev/typescript/` `library/dev/python/` | rules and references per language |
| `library/dev/react/` `library/dev/vue/` `library/dev/svelte/` `library/dev/frontend/` `library/dev/gpui/` | rules and references per framework |
| `library/qa/general/` `library/qa/references/` `library/qa/agents/` | rules, skills, references and agents for testing |
| `library/questions/` | the decision questions |
| `library/decisions/` | the decision methods |
| `library/models/` | language and classifier models, prices, windows |
| `library/subscriptions/` | `claude-sub` and `codex-sub` |
| `library/boardy/hooks/` | the board's built-in and starter hooks |
| `library/docs/` | the in-app docs |
| `library/changelog/` | the changelog, byte for byte with `CHANGELOG.md` |
| `library/memory/` | memory scopes |
| `library/tools/shell/` | the shell proxy and its rules |
| `library/web/` | fetch and search settings |

## bench

| Folder | What lives there |
|---|---|
| `bench/cmd/` | the dispatcher every suite runs through |
| `bench/kit/api/` `states/` | measuring a classifier over an api: latency, reruns, sweeps; `states/` holds the embedded states |
| `bench/kit/corpus/` `recorded/` | reading, gating and scrubbing a corpus; `recorded/` finds recorded states |
| `bench/kit/harness/` `task/` | running an agent turn: plan, run, parse the claude, codex and tofu output, report |
| `bench/kit/report/` `gen/` | the bench report: pages, figures, history; `gen/` builds it |
| `bench/gates/flows/` | driven flows over the built binary |
| `bench/gates/parity/` | parity between two builds, from `bench/gates/parity/cases/` |
| `bench/gates/treewide/` | the tree-wide test gate |
| `bench/suites/agent/fanin/` | fan-in of sub-agent results |
| `bench/suites/agent/longsession/` | a long session against a live host |
| `bench/suites/agent/orchestrator/` | orchestrator timing |
| `bench/suites/agent/phase/` | phase labels against hand labels |
| `bench/suites/agent/rivals/` | tofu against the vendor command lines |
| `bench/suites/agent/sessiontime/` | session wall time, tofu against claude |
| `bench/suites/agent/thread/` | one thread across turns |
| `bench/suites/agent/turn/` | turn mechanisms compared and replayed |
| `bench/suites/context/cache/shrink/` `keep/` | cache shrink; `keep/` what it keeps |
| `bench/suites/context/cache/ttl/` | cache time to live |
| `bench/suites/context/forkcache/` | the cache across a fork |
| `bench/suites/context/learn/` `jobs/` | learning; `jobs/` its jobs |
| `bench/suites/context/linenumbers/` `gen/` | line numbers in tool output |
| `bench/suites/context/memory/` | memory recall |
| `bench/suites/context/memtree/` | the memory tree |
| `bench/suites/context/prefix/` `gen/` | prompt prefix reuse |
| `bench/suites/context/recall/` `gen/` `standing/` | recall across checkpoints and forks; `standing/` standing orders |
| `bench/suites/context/schemas/` `gen/` | tool schema cost and deferral |
| `bench/suites/context/thrift/` | bytes saved by cutting redundant prose and tool output |
| `bench/suites/context/tokencount/` `gen/` | token counting and its correction |
| `bench/suites/context/tokens/` `gen/` | tokens per prose and pinned text |
| `bench/suites/context/toolerrors/` `gen/` | tool error shapes |
| `bench/suites/context/transform/` | edit transform arms |
| `bench/suites/judge/ask/` `server/` `server/gen/` | the ask decision; `server/` over a server |
| `bench/suites/judge/calibration/` `gen/` | calibration floors and rates |
| `bench/suites/judge/cost/` | decision cost against a regular expression |
| `bench/suites/judge/judge/` | request open and task kind |
| `bench/suites/judge/picker/` | the account picker over recorded readings |
| `bench/suites/judge/promote/` `gen/` | promotion |
| `bench/suites/judge/readworth/` | whether a read is worth it |
| `bench/suites/judge/sift/` | shell sift against rtk |
| `bench/suites/judge/stopcheck/` | the stop check |
| `bench/suites/judge/wording/` | question wording |
| `bench/suites/perf/boardy/` `replay/` | board speed; `replay/` replayed |
| `bench/suites/perf/cli/` | verb speed |
| `bench/suites/perf/minimal/` `overlay/` | a minimal run in a cage; `overlay/` its memory profile |
| `bench/suites/perf/profile/` `floor/` | idle profile; `floor/` its floor |
| `bench/suites/perf/scale/` | fan-out and wall time at scale |
| `bench/suites/perf/serve/` | the host under load |
| `bench/suites/perf/startup/` `frame/` `gen/` | startup; `frame/` the first frame |
| `bench/suites/perf/tui/` | interface render speed |
| `bench/suites/rules/enforce/` | rule enforcement |
| `bench/suites/rules/mutate/` | mutation testing |
| `bench/suites/rules/planspawn/` | plan before spawn |
| `bench/suites/rules/reach/` | rule reach |
| `bench/suites/rules/review/` | review measures against baselines |
| `bench/suites/rules/rules/` `batched/` `fire/` `gen/` | rules; `batched/` asked in batches, `fire/` which fire |
| `bench/suites/rules/testquality/` `count/` `flake/` `flakerun/` `mutrun/` | test quality: dead tests, flakes, mutation runs |
| `bench/suites/tools/browser/` `airbnb/` `airbnb/cmd/` `imageprobe/` `output/` `shots/` `tasks/` | the browser on real sites |
| `bench/suites/tools/nativetools/` | native tools against ours |
| `bench/suites/tools/search/` | search |
| `bench/suites/tools/searchlarge/` | search over a large tree |
| `bench/suites/tools/shortlist/` | the tool shortlist |
| `bench/suites/tools/sweep/` `gen/` | sweep findings |
| `bench/suites/tools/symbols/` | symbols against a ripgrep pattern |
| `bench/suites/tools/tools/` `candidates/` `corpus/` `linewidth/` `report/` | tool against tool |
| `bench/suites/tools/wrongpath/` | a wrong path given to a tool |
