# Changelog

Kept by hand, in the shape of keepachangelog.com, and versioned by semver.

**`.local/boji/diagram.html` is updated in the same edit as every entry below.** It draws what runs where, keeps a version selector so an older shape can be read back, and marks in green what changed in the selected version. It exists because too much moves in a night to hold in one head.

Tofu is personal and not released, so the public interface that a version promises is the command line and the file formats: the verbs and their flags, the exit codes, the catalog schema, the ledger row schema and the policy schema. A version says what changed for someone driving the binary or reading its files, not what changed inside it.

The minor number carries breaking changes, which is what 0.x means, and **there is never a 1.0.0**. The owner decided that on 2026-09-19: this stays 0.x forever, so the promise the version makes is the one 0.x already makes, that anything can break on a minor bump.

## Unreleased

### Added

- **Two hook events only tofu has.** `GateVerdict` runs after the gate decides on a call and can turn an ask into allow or deny, or a deny into ask, never a deny into allow. `SubagentSpawn` runs before a sub-agent starts and can refuse it or narrow its owns, never widen them. See `tofu docs hooks`.

- **`tofu session trace` lists every hook run** with its event, call, exit code, time, decision and stderr, and names the relaxation that let a call over `risk_ask_at` run.

### Changed

- **A sub-agent's row says what it is doing:** "waiting for the orchestrator's answer", "running bash, 12m" or "building, 12m". It says "stalled" only when no output, step or request has moved for the new `subAgentWatchSeconds` setting (default 600).

### Fixed

- **A resumed chat draws each sub-agent report as a report row**, never as something you typed.

- **`tofu session trace` on a continued session lists the sub-agents that kept working in the session before it**, marked with the session they were recorded in.

- **`tofu --continue` opens in under a second on a long chain of sessions**: 0.9 s where it took 12 s on a chain of 21 sessions with 84 sub-agents. Each session file is read once at open.

- **A continued or resumed conversation over the context target forks before its first request**, instead of sending the whole history first.

- **Shift+Enter adds a line past the eighth.** The input stops growing at 8 rows and scrolls; before, every line after the eighth was joined onto it.

- **A PreToolUse hook that asks before a sub-agent's call asks the lead**, as the gate's ask does, instead of refusing the call.

- **A finished sub-agent call no longer reads as open**, so a 1 ms write no longer shows as "stalled write, open 22m".

- **`SessionEnd` hooks fire when `tofu drive` exits**, and `SessionStart` fires with source `compact` after a compaction.

## 0.5.5 - 2026-10-06

Sub-agents you can trust across turns and resumes, and config the model cannot change alone.

### Changed

- **`--continue` and `/resume` restore the session's sub-agents.** `subagents` lists the ones that ran before the resume, with how each ended, and `message` can resume a finished or parked one with its conversation.

- **A `write`, `edit` or `bash` call that changes tofu's settings, the hooks files it reads, or the hook trust file waits for you**, in auto mode too, and a sub-agent's is refused.

### Fixed

- **`ping` runs as asked, without rtk.** rtk's ping filter garbled accented output on Windows and cut the reply lines.
- **A link in the chat no longer leaves an underline running to the screen's edge.**

- **Sub-agents spawned after earlier ones each get their own report row.** A new turn's spawns no longer take the names of sub-agents from earlier turns, so stopped sub-agents show as parked and old reports are not drawn twice.

### Changed

- **A sub-agent that only runs a command, reads or researches is spawned without owns** and starts at once. Any write or edit it tries is refused with the reason, so the lead no longer invents placeholder files to start one.

## 0.5.4 - 2026-10-06

Sub-agents that ask the orchestrator, forks at the right point, long builds in the background, and a trace that keeps everything.

### Fixed

- **"Thinking" shows only while the lead's model thinks.** An open turn where only sub-agents work, or the lead writes, says "working".

- **A long command moves to a background shell instead of being killed.** A bash call still running after 30 s returns with the shell's name and its output so far, and the command keeps running. `shell wait` waits for it to end. Sub-agents get the `shell` tool wherever they get `bash`. A shell's row shows the tail of a log file the command names.

- **A fork happens near the target, and keeps the steps the model was in.** A ceiling set with `TOFU_CONTEXT_CEILING` or `--context-ceiling` used to fork at 25% of it; it now forks at 80%, as a ceiling taken from the model's window does. A fork keeps the newest step whole when it fits, and the trace records the count that decided it.

### Added

- **`tofu serve --stdio`** lets another program drive tofu over JSON-RPC on standard input and output (`tofu.host/1`). It sends typed items with session, turn, agent and seq, carries approvals when asking is on, and refuses a second writer with `session.busy`. `tofu serve --schema` prints the schema.

### Changed

- **A sub-agent never asks you.** When Jev would ask about a sub-agent's call, the ask goes to the orchestrator, which answers allow or deny with the verdict in hand. The sub-agent waits up to 5 minutes for it.

### Added

- **A second app on a session another tofu is writing opens read only** and fires no cron job, so a loop fires once per interval however many apps are open.
- **`oneTurnPerProject`**, on by default, refuses a turn while another session in the same project runs one. Turn it off to run several sessions in one repository at once.

### Fixed

- **A stored key split across two streamed pieces of a reply is masked.**

## 0.5.3 - 2026-10-06

Sub-agents you can follow and stop, hooks, and auto mode.

### Fixed

- **Ctrl+C with the lead idle and sub-agents working asks first**: "this will stop 2 sub-agents, press Ctrl+C again to confirm". A second press within 3 s stops them; otherwise nothing stops. Ctrl+C while the lead works still stops only the lead, at once.
- **A sub-agent whose tool call has been open past the longest bash deadline shows as stalled**, with the call and how long it has been open.
- **A sub-agent's report that arrives while the lead is requesting is drawn**, once, after the lead's words. It used to be lost.

- **While the lead's sub-agents work, the app says "waiting on N sub-agents"** with a live count, instead of "cooked for". "Thinking" shows only when the lead's own model answers, not while it waits on a request or runs spawns, and a sub-agent's stats no longer rename the lead's model.

### Changed

- **Auto mode is the default.** Jev decides at every gate and the work does not stop for you: a call it would ask about runs and is recorded, and a call it denies is refused. `tofu settings set gatePrompt ask` makes it wait for you. A hook that asks, a rule override, and the model changing a setting still ask you in both modes.

### Fixed

- **A stored key is masked in every event tofu sends to the interface**, not only in tool calls, results, failures and asks.

### Added

- **Hooks.** Claude Code's and codex's hooks run under tofu as they are, read from `.claude/settings.json` and `~/.claude/settings.json`, plus tofu's own `.tofu/hooks.json`. PreToolUse, PostToolUse, UserPromptSubmit, Stop, SubagentStop, SessionStart and SessionEnd fire for the lead and every sub-agent. A project's hooks run only after you trust them once, and are asked about again when they change. `tofu hooks` lists every hook, its source, its trust and its last result.

## 0.5.2 - 2026-10-06

Faster handoffs and a frontend that can trust what it is sent.

### Fixed

- **A `library/` folder at the root of a repository is no longer read as tofu's rules.** A project's own library now lives in `.tofu/library/`.
- **A session open for writing is locked.** A second tofu on the same session gets a busy, read-only view and never writes to it.

- **A key typed into a prompt is masked in `session.json` too**, and in sessions converted by `tofu migrate`, as it already was in `events.jsonl`.
- **Dialogs in a Windows 10 console draw in place.** `/keys`, ctrl+p and the model picker no longer leave stray text at the left edge or push their title out of the box.

### Added

- **A prompt starting with `!` runs as a shell command** in the project, with no model call. The chat shows its output, keys masked, and the next prompt carries it to the model. Esc stops it.
- **Ctrl+P searches every prompt you sent**, across sessions and projects. Enter puts the prompt in the composer without sending it.

### Changed

- **A reply that only starts sub-agents ends the lead's turn**, with no extra request to announce them. Sub-agents started in one reply are named in the order the reply lists them.
- **A frontend sub-agent carries only the rules its task touches.** A rule can declare `touches:`, and a sub-agent working on something else does not get it. About 8 KB less per frontend sub-agent request.
- **The lead's prompt carries rules about what to build, not how to write it.** Rules for writing, testing and building code reach only the sub-agent that writes it, unless the lead runs alone.

## 0.5.1 - 2026-10-05

Setup by role and undo.

### Added

- **Cron, loops and goals.** `/loop 10m <prompt>` repeats a prompt, `/goal <prompt> --until "<command>"` keeps working until the command passes, and `/cron` makes, lists, edits, pauses and deletes jobs. Every change to a job is kept as a version with its reason, and the lead can change a job under the `cron_edits` rule.
- **`/compact`** shrinks old tool results to a handle between turns, and the shrink survives a restart.
- **`/resume`** opens a picker of this project's sessions, and `/resume <name>` resumes one directly.
- **`/keys`** lists every key the app answers to.
- **`tofu undo [N]` and `/undo [N]`** put back the files the last turns changed, including changes made through bash. A file you edited since is left alone unless `--force`, and `--dry-run` shows what would change.
- **Ctrl+G** opens the prompt in `$VISUAL` or `$EDITOR`.
- **A dropped or pasted image path** attaches the image.
- **Ctrl+V on Linux and macOS** reads the clipboard: `wl-paste`, `xclip` or `xsel` on Linux, `pbpaste` and `osascript` on macOS.
- **`fetch` reads a long page 300 lines at a time**, with `offset` and `limit`, from one request.
- **`write` takes `append: true`.**
- **`tofu rules index --role orchestrator|sub-agent`** shows the rules each one gets.
- **The drive's `requests` step** prints each request's tool count, tools hash and tool choice.
- **`search` returns the whole function, method, class or type in TypeScript, JavaScript, Python and Rust**, as it does in Go, so the model does not read the file again.
- **The lead gets a check on each running sub-agent** every `subAgentCheckSeconds`, 30 minutes by default: its steps, the files it changed, its gate, its last tool and its cost. tofu writes it with no model call.

### Changed

- **A first start with nothing set up opens a two-step setup screen**: pick the language model (Claude subscription, Codex subscription or a Meta key), then the classifier (OpenRouter or TypeSafe key). A key is typed into the screen and shown by its shape. The chat opens when both are done.
- **`tofu login` takes a role**: `tofu login llm claude-sub|codex-sub|meta`, `tofu login classifier openrouter|typesafe`, `tofu login search brave`. The old forms exit 2 and name the new one. `tofu logout <role> <provider>` removes what a login stored. While you type or paste a key, the field shows its shape (prefix, last four, length) and never the key itself. `tofu login --status` groups by role. Breaking: in `--json`, `role` is now `llm`, `classifier` or `search`.
- **The lead no longer re-checks a sub-agent's work by default.** It takes a finished report as the result unless the report says something failed, and a clean report ends the turn with no tool call. `tofu settings set verifySubAgents true` or `tofu rules restore verify_sub_agents` turns the check back on.
- **A turn that fills the context window** shrinks its oldest tool results and asks once more, instead of ending.
- **The fork point follows the model's window**: 144,000 tokens on a 200k model, where it was 63,000.
- **An empty answer, or a stream cut by an overloaded server, is retried once.**
- **An OAuth refresh survives Esc**, and a 401 on a token that looks fresh renews it and sends once more.
- **A turn is warned before its step cap**, and its last step must answer. A sub-agent stops after an hour.
- **Bash** keeps a timed-out command's output, holds tofu's keys out of the command's environment, sets `AI_AGENT=tofu`, never waits on a git prompt, and decodes a Windows console code page.
- **`edit` keeps CRLF and a BOM.** A refused edit names the lines to read instead of pasting the file. A partial read covers only the lines read.
- **`write` over an existing file writes in place**, so it keeps the mode and a symlink, and works while another process reads the file. A write that replaces code with a placeholder such as `// ... rest unchanged` is refused.
- **A path whose link leads outside the project is refused**, and `glob` and `search` skip folder links.
- **`glob`** takes double-star patterns and `{a,b}`. **`search`** shows a match on a very long line, skips files over 16 MB, and goes on past a file it cannot read.
- **A long tool result is stored whole**, so `artifact_fetch` can read its middle.
- **The model picker** never offers Fable, opens on the model in use, and keeps its header in cmd and Git Bash.
- **A plain cmd window** shows colour instead of raw escape codes, and tofu leaves the console's code page as it found it.
- **A library sub-agent** gets the skills its definition names.
- **`tofu doctor`** reports the shell sift the way a run uses it.

### Removed

- **The `compaction` setting**, which changed nothing.

## 0.5.0 - 2026-10-05

The first public release: install with one command on Windows, Linux or macOS, and a lead that spends far fewer tokens for the same work.

### Added

- **Install scripts and `tofu update`.** `install.sh` for Linux and macOS and `install.ps1` for Windows install the latest release into `~/.local/bin` after checking its checksum. `tofu update` replaces the binary with the latest release, and `tofu update --check` only says whether one exists.
- **Releases from the `release` branch**, built for Windows, Linux and macOS on amd64 and arm64.
- **The browser relay on Linux and macOS.** `tofu browser install` registers tofu with Chrome, Chromium, Brave and Edge. `tofu doctor` gains a browser section that says, per browser, whether tofu can reach it.
- **Rule overrides.** `tofu rules off` and `tofu rules add` take `--reason`. `tofu rules overrides` lists every override, and `tofu rules restore` removes one. When a rule blocks what you asked for, tofu names the rule, gives its reason, and asks: 1 this project, 2 everywhere, 3 no.
- **A page check in one call.** `browser_do` takes a list of steps. Each step can check focus, an attribute, a name, the url, text, or computed style, and can turn reduced motion on. `tofu browser batch` runs the same list from a file.

### Changed

- **Shell output through rtk by default**, when rtk is installed. Background, piped and redirected commands run as written, and `use: off` in a `tools/shell/proxy.yaml` turns it off. `tofu doctor` says whether rtk was found.
- **The lead carries less.** It gets the design rules that shape its brief, not the rules for how code is written. Tools a project cannot use are dropped, and a sub-agent loads its references when it needs them. The lead skips its own check pass after a single sub-agent's clean report whose checks passed.
- **The gate reads the project's own checks**: a check run through rtk, or the project's `typecheck` or `test` script, counts. A sub-agent is judged only on errors in the files it owns. In a Vue or Svelte project, the typecheck tool runs `vue-tsc` or `svelte-check`.
- **Tabs tofu opened close** when the run, the sub-agent or the app that opened them ends.

### Removed

- The old unused update check in `internal/update`.

## 0.5.0-rc-fix24 - 2026-10-03

Tabs tofu opens close when it is done with them, and a frontend rule that measurably changes what the agent writes.

### Changed

- **Tabs tofu opened in Chrome close when the run ends**, and a browser sub-agent's tabs close when it finishes. In the interface they close when the app quits. A person's own tab is never closed. `tofu run` now cancels its turn on the first Ctrl+C so this still happens; a second Ctrl+C ends it at once.
- **`fe_url_state` puts history first**: a filter, tab, sort or page change pushes a history entry and a search replaces it, so Back returns the view before.

## 0.5.0-rc-fix23 - 2026-10-03

A library per language and per frontend framework, each rule tied to the check that catches it.

### Added

- **go-dev and py-dev**, sub-agents for Go and Python beside ts-dev and rust-dev. go-dev reads go.mod and the project's lint config and finishes on `go vet` and `go test`. py-dev reads pyproject.toml and finishes on `ruff check` and `pytest`, through the project's own runner. Each comes with rules naming the vet analyzer, linter or ruff code that checks them, and four references.
- **Frontend rules for every framework**: motion, interaction, forms, accessibility, performance and layout, each naming the stylelint, axe, a11y lint or browser check that catches it. A project that has its own components never ships the browser's own widgets: no `alert`, `confirm` or `prompt`, no native date, time or colour pickers, no `title` tooltips, and no native `<select>` when it ships its own.
- **React, Svelte and Vue rules** that reach only their own projects: React effects and purity checked by eslint-plugin-react-hooks, Svelte 5 runes and SvelteKit checked by the Svelte compiler and svelte-check, Vue's Composition API checked by eslint-plugin-vue and vue-tsc.
- **A `framework:` key for rules**, read from package.json, including per app in a monorepo. `framework:` and `language:` take a list. `tofu rules index` names the framework that made a rule fire.
- **A Rust rule for input-depth recursion**: a loop over an explicit stack, or a depth limit.

### Changed

- **A gate check counts through a runner or a project script**: `uv run pytest`, `python -m pytest`, `make test` whose recipe runs `go test`.
- **A gate failure is a real failure**: a typecheck that finds an error and a test run with a failure send the sub-agent back. A project with no tests is not held to a test check.
- **A sub-agent sent back stays one sub-agent**: one line in the chat saying "sent back: test did not run", and a report that says "sent back once" instead of "escalating".
- **`scope:` on a rule matches the paths a sub-agent owns.**

### Fixed

- **The browser relay hands over to a newly installed tofu**, so the extension updates without `tofu browser install`.

## 0.5.0-rc-fix22 - 2026-10-02

A Rust sub-agent, and sub-agents that are held to their own checks.

### Added

- **rust-dev**, a sub-agent for Rust. It reads Cargo.toml, the toolchain and the project's own gate first, and finishes on `cargo clippy` and a scoped `cargo test`. Thirteen Rust rules come with it, each naming the clippy lint that checks it, and four references: verification, unsafe, errors and ownership, async.
- **An agent definition can name its gate**, `gate:` in its front matter. ts-dev's is `typecheck, test` and rust-dev's is `cargo clippy, cargo test`. A sub-agent that changed a file in its language is sent back until every check in its gate ran after its last edit and passed.
- **`tool_pick`**, the tool guidance every call carries, is now a shipped rule. `tofu rules list` shows it, and a project can switch it off or replace it. It names the pairs explicitly: edit over `sed -i`, glob over find, search over grep, read over cat, and the right checks for TypeScript and Rust.

### Changed

- **A sub-agent changes source with edit or write.** A shell command that writes a source file, such as `sed -i`, `perl -pi`, a redirect or `cp`, is refused with the file named. Formatters, temp files and lockfiles are unchanged.

### Fixed

- **A tab opened with `+` joins tofu's group only when you were in it.** From a tab outside the group it opens outside, as Chrome would without tofu.

## 0.5.0-rc-fix21 - 2026-10-02

The lead knows what its sub-agents are doing, and the screen stops dropping text.

### Added

- **The lead lists its sub-agents.** A `subagents` tool beside `spawn` and `message` gives each sub-agent's state, how long it has run, its steps, its effort and the last tool it called, without waiting for any of them. Asked whether a sub-agent still runs, the lead checks instead of guessing.

### Changed

- **The qa agent runs `test` and `typecheck`** on the warm runners instead of vitest and tsc through bash.

### Fixed

- **A line under the chat no longer loses its middle** in cmd.exe and PowerShell. With no TERM set, tofu now draws for xterm-256color and keeps the colour profile it detected. A TERM you set is left alone.

## 0.5.0-rc-fix20 - 2026-10-02

Sub-agents work in the background and the lead stays with you, and a large TypeScript project answers in seconds.

### Changed

- **A sub-agent runs in the background.** `spawn` returns at once, the lead's turn ends on its own terms, and the sub-agent keeps working. When it finishes, its report starts a new lead turn, drawn in the chat as `⟩ report [&name] finished`.
- **You can talk to the lead while sub-agents work.** A message typed while only sub-agents run starts a lead turn at once. One typed mid-turn reaches the lead at its next step.
- **The lead can message or stop a sub-agent from an earlier turn.** `message` reaches a running sub-agent at its next step, and resumes one that has ended with its conversation. `message` with `stop` parks one sub-agent and leaves the others running.
- **Esc and the first ctrl+c stop only the lead's turn.** Sub-agents keep running until they finish, the lead stops them, or tofu quits.
- **The header clock counts the whole session line** and no longer restarts when a session continues into a fork.
- **`subAgentsPerTurn` now limits sub-agents running at once.** The key is unchanged.
- **The lead starts each sub-agent at low effort** and settles the open choices in the brief. It raises the effort when a sub-agent fails the same check twice, returns work it would not accept, or breaks its rules.

### Added

- **A `typecheck` tool on a warm TypeScript checker,** started with the session. An edit on a large project returns before its check.
- **A `test` tool on a warm vitest runner** per package. Given a source file, it runs the test beside it (`ls.ts` runs `ls.test.ts`). A runner that misses its deadline is replaced and does not hold the next call.

### Fixed

- **The shells screen and `tofu shells log` show a log whose first line is blank,** such as `pnpm dev`.
- **Owns accept a TanStack route name** like `routes/components/$slug.tsx`, and a path with `..` that stays inside the project.
- **A sub-agent's shell writes to `/tmp` reach the real temp directory.** A write or edit tool call on `/tmp/x` is refused rather than creating `tmp/x` inside the project, for the lead too.
- **A first search in a large repository answers in under a second.**
- **An owned directory named without a trailing slash owns the files inside it.**

## 0.5.0-rc-fix19 - 2026-09-30

### Fixed

- **The browser sub-agent starts when `browser` is set to read.** It gets the tools read mode offers and leaves out the ones only drive mode has. A sub-agent that names a tool tofu has never heard of is still refused.
- **`browserCursor` is listed in the settings docs.**

## 0.5.0-rc-fix18 - 2026-09-30

A browser that finishes the task on sites it was never tuned on, and a motion capture that says what blinks.

### Added

- **`tofu browser motion capture <scenario.json>`** records takes of an interaction, frames and a per-paint trace, into `~/.tofu/motion/<take id>/`. With no `watch` list it watches every visible element and prints **What blinked**: each element that changed and changed back, by selector, role and name, with its time, duration, values and frames. The same report reaches the agent through `browser_motion`.

### Changed

- **A page answers once it is useful** and later changes arrive with the next result, so a browsing turn waits far less.
- **Each act step names where it went:** the url it reached, an `expect_after` check that held, and what a select, fill or checkbox now reads.
- **A click lands on the largest visible part of its target**, and falls back to the element's own `click()` when the mouse misses.
- **A site recipe is learned from the url** of a finished task and given to the browser sub-agent on the next visit.
- **A start url appears in the brief only when the task names one.**

### Fixed

- **A password fill never shows or sends its value.** The act line gives its length only.
- **An act that times out says which step and after how long**, reconnects, and the next act works. It no longer reports that the extension is not connected.
- **The relay no longer freezes** when a client times out, and restarts itself when a new tofu is installed.
- **An empty field from a GPT model is read as absent**, so its act calls are not refused.

## 0.5.0-rc-fix17 - 2026-09-29

One look for every command, `--json` that always means one JSON document, and shells, settings and the browser that do what they say.

### Changed

- **Every verb prints the same way:** a title with a verdict, labelled sections, `✓ ● ○ ⚠ ✗` marks, `~ + -` receipts for a write, and a `→` line with the next command. Colour follows the terminal, and `NO_COLOR` turns it off.
- **`--json` works on every verb that reports state** and prints one document: `{tofu, verb, ok, at, data, problems}`, with the same exit code as the text form.
- **A long line wraps and is never cut.** A command in a hint always prints whole.
- **An unknown verb prints one error line and `→ tofu help`**, not the whole usage text.
- **Settings save to the scope the header names.** shift+tab switches between global and project.
- **The API key input is its own dialog** and no longer draws over the model picker.
- **`browser_do` waits for its tab to load**, reuses its own tab, follows a tab the page opens, and answers from every page it saw.
- **The undo after `tofu rules add --replace`** puts the old rule text back.
- **`tofu models reload` checks npm for the latest Claude Code and Codex releases** and raises the versions tofu claims. It stores them in `subFingerprint` in `~/.tofu/settings.json`, where you can also raise them by hand. The higher of that value and the built-in one wins.
- **`--json` on a usage error prints one JSON error** and exits 2.

### Fixed

- **A new Claude model no longer fails with `claude_code_version_too_old`.** tofu claims Claude Code 2.1.284 and Codex 0.159.0. When Anthropic asks for a newer version, tofu saves it and retries once.
- **Every process tofu starts ends when tofu ends**, even when tofu is killed.
- **A quit ends only this tofu's shells.** Shells left by an older tofu show as leftover in the shells screen, where you can end them.

### Removed

- **The hide welcome art setting** and its Startup category.

## 0.5.0-rc-fix16 - 2026-09-28

Faster, surer browsing, models that arrive by themselves, keys that stay hidden, and a reload that re-reads everything.

### Changed

- **A browser step no longer loops on a page it wrongly thinks changed.** A covered link stops after 3 tries and says why, the page settles after a click, and `values` matches a field by label, placeholder or name in any case. On his recorded session this cuts Jev decisions per finished action from 48 to 7.
- **`browser_do` takes a whole goal and answers it:** `browser_do {goal, url}` opens its own tab, types what the browser model writes, and returns the answer with links first.
- **No share step.** Tofu can read and drive any open tab. The tabs it works in sit in an orange tofu group, the icon shows reading, acting or idle, and `browser` is on by default.
- **Fable and Astra models are allowed.** No text tofu shows names a person or a date.
- **Instruction files is a choice:** agents-first, claude-first or both, in settings or `tofu settings set instructionSources`.
- **Settings read on every turn no longer say they need a restart.** Only `persistentRegistry` does.

### Added

- **`tofu models reload`** asks each signed-in account which models it serves, and adds the new ones, such as `claude-sub/claude-sonnet-5-5`, into `~/.tofu/catalog`. The model picker shows where each model came from, f5 there reloads, and a stale list reloads itself at start.
- **Meta Muse Spark:** `tofu login meta`, then `meta/muse-spark-1.3`. The contributor models are marked as models Meta may train on. You can also pick one in the model picker and paste the key there.
- **Settings `browserModel`** (falls back to `modelTier.dumb`, then `worker`) and a **Classifier model** row, which picks Jev on OpenRouter or TypeSafe.
- **`tofu reload` and `/reload`** re-read settings, rules, skills, sub-agents, models and keys, and print what was added, removed or changed.
- **`tofu browser bench`** times each phase of a browser step.

### Security

- **API keys live in the credential store,** never in `~/.tofu/.env`. On first start that file's keys move there and the file is removed. `tofu login openrouter|typesafe|brave|meta` stores a key, and `tofu login --status` lists them by their last four characters.
- **A key value never reaches a tool result, the chat, the session log or the ledger.**

## 0.5.0-rc-fix15 - 2026-09-28

Tofu reads and drives the Chrome tabs you share with it, from inside its own binary.

### Added

- **`tofu browser install`** writes the tofu extension to `~/.tofu/browser/extension` and registers `tofu.exe` as Chrome's native host. There is no second program and no open port. Load the folder once in `chrome://extensions` with Developer mode on.
- **The tofu extension** shares only a tab you pick from its icon, to read or to drive. It never attaches to other tabs, never navigates a tab, and never runs JavaScript or selectors that a model wrote.
- **`tofu browser`** lists the shared tabs. `tofu browser open <url>` starts a background tab that tofu owns, and `tofu browser close <id>` closes only such a tab.
- **The setting `browser`,** off by default. `read` gives the model `browser_tabs` and `browser_read`. `drive` also gives it `browser_do`, where Jev picks each step's action and target from the page, and the model passes the text for any field in `values`.
- **The setting `browserChooser`:** jev, the default, or model, which gives the model `browser_act` to take each step itself. `browserSteps` caps the actions, 30 by default.
- **`tofu docs browser`** explains all of it.

### Changed

- **A choice set to off is drawn as a choice in settings,** not as an on/off switch.

## 0.5.0-rc-fix14 - 2026-09-28

Tofu knows how it works and how to change itself.

### Added

- **`tofu docs`** prints an index of common asks, each with the command that does it. `tofu docs <topic>` prints a page, and `tofu docs "a few words"` finds the closest. There are 11 pages: start, files, settings, rules, agents, models, skills, instructions, gate, sessions and doctor.
- **The model reads the same docs through a `tofu_docs` tool,** and is told to change tofu only with the command a page names. `tofu run --no-docs` and `tofu drive --no-docs` turn this off.
- **`tofu rules add [--global] <id> "<text>"`,** plus `tofu rules off <id>` and `tofu rules remove <id>`. Global rules live in `~/.tofu/rules`, project rules in `.tofu/rules`. `tofu rules list` shows where each rule comes from.
- **`tofu agents add <name> --description d --model source/model`,** plus `tofu agents set <name> <source/model>` and `tofu agents remove <name>`, globally with `--global` or in the project.
- **`tofu browser install`** registers tofu as Chrome's native host and writes the tofu extension to load by hand. The model's browser tools come in the next release.

### Changed

- **The prompt, `tofu rules list` and `tofu reload` read the same rules:** shipped, then global, then the project given by `--dir`.
- **`tofu library` exits 1** when a setting or a verb has no docs page.

## 0.5.0-rc-fix13 - 2026-09-28

Tofu runs on its own setup, not on another harness's.

### Changed

- **From your home directory tofu reads only `~/.tofu`:** `~/.tofu/AGENTS.md`, `~/.tofu/skills` and `~/.tofu/agents`. It no longer reads `~/.claude/CLAUDE.md`, `~/.claude/skills`, `~/.claude/agents`, `~/.agents/skills` or `~/.agents/agents`, whatever `instructionSources` and `agentSources` say.
- **A project's own `AGENTS.md`, `CLAUDE.md`, `.claude` and `.agents` folders are still read,** and AGENTS.md still comes before CLAUDE.md in one folder.
- **Instruction files are looked up between the working directory and its git root.** With no git, only the working directory counts, so a `CLAUDE.md` in a parent folder is no longer sent.
- **The welcome art code no longer carries the old cover screen.** Nothing changes on screen.

## 0.5.0-rc-fix12.1 - 2026-09-28

The welcome art looks as the cover did.

### Fixed

- **The welcome art in an empty chat draws cell for cell as the old cover did.** The theme no longer paints a background behind its blank cells, and the orbiting dot keeps its old colour.
- **The dot moves one step per pulse.** The pulse timer starts once per session, not twice.

## 0.5.0-rc-fix12 - 2026-09-28

One screen to start, and sub-agents that talk to the orchestrator instead of guessing.

### Changed

- **A new session opens straight on the chat.** The tofu cat and wordmark sit in the empty transcript until the first message. The separate cover screen is gone. `hideIntroduction` now reads "Hide welcome art".
- **A table in a message sizes to its content,** with a bold header and faint separators, and no longer stretches across the screen.
- **Every sub-agent is told what it is:** one worker in a larger build, with its brief as its whole job and its paths as its whole reach.
- **A sub-agent's `sleep` over 5 seconds is refused,** and so is its `kill`, `pkill` or `taskkill` of a process it did not start.
- **A finished sub-agent releases the paths it held,** so the next spawn on them is not refused.
- **The shell ownership check follows a `cd`** in a command, so `cd src/api && sed -i ... x.ts` counts as `src/api/x.ts`.
- **The design rule carries its template sections** and no longer sends the orchestrator looking for a template file.

### Added

- **Sub-agents can ask the orchestrator.** The `ask` tool is answered from the orchestrator's model and the whole turn so far, for example "no, bash-2 serves it on 3003". If no answer comes, the sub-agent's own default is used, marked assumed.
- **The orchestrator can message a finished sub-agent** with more work or a correction. The sub-agent keeps its history. This works within one turn.
- **Asks and messages read as a conversation on the sub-agents screen,** in both agents' feeds, never in the chat.
- **Setting `thinkingSummary`:** summarized, the default, or omitted, which sends no thinking request.

## 0.5.0-rc-fix11 - 2026-09-27

Done looks done, code looks like code, and sub-agents stop waiting on each other.

### Changed

- **Finished sub-agents on the chat's settled line are drawn in a muted green,** so done and running read apart.
- **Code in a message carries a faint `│` rule on every line,** fenced or indented, and a fence with no language still gets a guessed highlight.
- **An unset model tier no longer prints a notice in the chat.** The sub-agent runs on the orchestrator's model, and `tofu agents` still says so. A definition file that fails to read still shows.
- **A sub-agent builds and checks only the paths it holds** and never sleeps waiting for a sibling's file. The orchestrator checks the whole after everyone answers.
- **The orchestrator reports the checks it ran as a markdown table.**

## 0.5.0-rc-fix10 - 2026-09-27

One wave again, and the status line counts instead of repeating names.

### Changed

- **The orchestrator writes the shared contract itself,** as a markdown doc naming who owns each shared module. Then it spawns every piece in one wave. It no longer spawns a foundation sub-agent first and waits, and nobody writes empty stubs for others.
- **The line above the composer reads `waiting on (3) sub-agents`.** The names stay on the chat's batch line.
- **The menu reads `shells (N)`** while N shells are running.

### Fixed

- **Turn 2 reads turn 1's cache** when turn 1's request mentioned tests. The test rules the request let in now go to its first message, not the system prompt.

## 0.5.0-rc-fix9 - 2026-09-27

Thinking is visible, a spawn batch is one line, and each agent gets only the rules that fit it.

### Changed

- **A spawn batch is one line in the chat:** `⠂ waiting on [&ts-dev-1] [&ts-dev-2]  1m 12s`. Finished sub-agents join one settled line below it: `✓ [&ts-dev-1]`, `✗` for failed, `○` for stopped.
- **Thinking shows on the sub-agents screen,** in each agent's own feed, the orchestrator's included. It is dim, with code folded to `...`, and never appears in the chat. Setting `showThinking` is on by default, and `t` toggles it. Tofu now asks Opus 4.7 and later for summarized thinking.
- **Sub-agents of the same kind wait for a warm cache.** The first one starts at once, and its siblings start when its first model call returns, 8 seconds at most.
- **A rule reaches only the agents of its domain.** qa rules go to qa, and dev rules go to the orchestrator and dev agents. The test rules (e2e_first, failure_modes_first and no_unit_test_after_code) also reach a dev agent whose work touches tests.
- **debug_loop:** find the root cause, add temporary logging, and read the dev server's output.
- **minimal_diff:** no throwaway compatibility code during a migration.
- **test_assertion:** call the code the way its users do.
- **evidence:** a number says whether it was measured, estimated or not known yet.
- **review_diff:** scope the change added on its own is removed, not approved.

### Added

- **A `research` sub-agent.** It is read-only apart from its report, puts a citation behind every finding, and writes a fixed report: verdict, findings, confidence, open questions, consulted. The orchestrator can spawn several at once.
- **New rules:**
  - always on: reuse_inventory, design_first, data_first;
  - for the orchestrator: plan_before_spawn, lesson_to_check, guard_context.
- **Design documents on request.** Asking for a PRD, HLD or LLD fires design_docs, design_layers, bounded_change, draft_not_approved and conflict_held. PRD, HLD and LLD templates come with them. None of these fire on ordinary coding.
- **A cassette reply takes `thinking`.**

## 0.5.0-rc-fix8 - 2026-09-27

The same run in less time: sub-agents run at the same time, and the wait, the port clash and the repeated reads are gone.

### Changed

- **Spawns sent in one message run at the same time** when their owns do not overlap. Their results come back in call order.
- **How many sub-agents a turn may spawn, and how deep, are settings:**
  - `subAgentsPerTurn` defaults to 10 and `subAgentDepth` to 2, under Turn in the settings menu.
  - The orchestrator can ask to change them with a settings tool, which always asks the person first.
- **One status line sits above the composer**, "waiting on [&ts-dev-3]", with a Dots3 spinner. Each sub-agent's live work spins on its own "spawning" line in the chat, in Dots8. Every spinner steps at 80ms.
- **A dev server start checks the port on both 127.0.0.1 and ::1.** It refuses a port held by a process tofu did not start, and names that process and a free port. `check_port` reports both addresses and who holds them.
- **A background start returns as soon as its port opens** or it prints a ready line, not after a fixed 10 seconds.
- **A stream that goes quiet for 60 seconds is sent again.** The retried answer shows once in the chat.
- **A file read earlier in the session and unchanged since is not refused as unread** by a later write or edit.
- **A sub-agent starts with the files its brief names already in its first message.**
- **A TypeScript write is typechecked** when the project has no typescript installed, through `bun x` or `npx`.
- **A sub-agent report** drops "learned nothing" and failures it retried and passed. A finished sub-agent ends done.
- **Tofu says sub-agent**, never child, everywhere.
- **A sub-agent's report renders as markdown**, with highlighted code.
- **The progress line spins until the turn ends.**
- **The orchestrator proposes changes** to your scripts, ports or config that you did not ask for, and does not make them.

### Added

- **Every request records `duration_ms` and `first_token_ms`** in the session's events.
- **`go run ./bench/orchestrator -session <folder>` prints where a session's time went.**

## 0.5.0-rc-fix7 - 2026-09-27

The orchestrator delegates, the chat is a conversation, and a sub-agent's work shows on its own screen as it happens.

### Changed

- **With sub-agents on, the orchestrator delegates.** It writes docs, plans and config freely. It may change 10 lines of source per turn for a small fix. Past that budget, a source write is refused, and the refusal names the sub-agent to spawn. A shell command that writes source, such as `sed -i`, is always refused for the orchestrator. Five orchestrator-only rules tell it to delegate, keep chat for conversation, announce each spawn, check a sub-agent's work and report short.
- **The chat is a conversation.**
  - Every tool call is one line, with `[expand]`. A click or ctrl+o opens the command, the gate's answers and the output.
  - A spawn is one line, "spawning [&qa-1] to <mission>".
  - Watch-only gate verdicts, the plan and a sub-agent's calls no longer appear in chat. The plan shows on the sub-agents screen.
- **The sub-agents screen shows a sub-agent's work as it happens.** Each call goes from running to done. The sub-agent ends done, failed or stopped. The spawn is its own card, with the brief rendered as markdown and "waiting on [&qa-1]".
- **Sub-agents are named by their definition**, such as `qa-1`, `ts-dev-2` and `sub-1`.
- **The status reads "waiting on [&qa-1]"** while the orchestrator only waits on a sub-agent. A cancel reads "cancelled at 15m 47s".
- **The edits sidebar is headed EDITS**, lists each file once with a count, and scrolls.

### Added

- **A TypeScript write or edit returns the project's own tsc errors**, including Node's type-stripping errors, such as a parameter property.
- **`shell stop|restart|logs <name>`** and **`tofu shells stop|restart`** stop or rerun a whole process tree. Killing a process that tofu started runs as a stop.
- **Esc on an empty composer stops the running turn.**
- **The library reference `verify-a-running-service`** covers starting a service, waiting for its port, calling every route, and stopping it. ts-dev and qa both name it.

### Fixed

- **A sub-agent is no longer refused on false paths**, such as `/dev/null`, `console.log` or a URL. qa can write the report it owns.
- **The shells screen updates during a turn.**
- **An old session's turns keep their own tool results** when resumed or migrated.
- **Only `tofu`, `tofu --continue` and `tofu migrate` move or copy project state.** Every other verb leaves it alone.
- **Ctrl+c no longer forks a stray session.**

## 0.5.0-rc-fix6 - 2026-09-26

Skills load from your own folders, and claude-sub haiku runs.

### Added

- **Skills.** Tofu reads skills from `.tofu/skills`, `.agents/skills` and `.claude/skills`, walking up to the repository root, then from the same folders under the home. The first name found wins. The prompt lists each skill's name and description, and the `skill` tool loads the body when the model asks for it. A sub-agent's `skills:` or `autoloadSkills:` load before its task. `tofu settings set skills off` removes the listing. The library ships no skills: its content stays in rules and references.

### Fixed

- **claude-sub haiku runs.** A reasoning effort is sent only to a model that takes one, so haiku, and the `@worker` or `@dumb` tier set to it, no longer fails with `400 This model does not support the effort parameter`. Shift+Tab cycles only the levels the chosen model takes, and on haiku the footer shows none.

## 0.5.0-rc-fix5 - 2026-09-26

Each sub-agent works with its own rules, references and model tier, and the footer shows every subscription in use.

### Added

- **Model tiers.** Set `modelTier.genius`, `smart`, `worker` and `dumb` with `tofu settings set`. A sub-agent names a tier as `@smart` in `.tofu/agent-models.yaml` or `.tofu/agents`. In a shared `.claude` file, `opus`, `sonnet` and `haiku` map to genius, smart and worker when those tiers are set. An unset tier runs on the orchestrator's model, and `tofu agents` says so.
- **`tofu run --show-prompt --agent <name>`** shows the prompt a named sub-agent receives.
- **`tofu drive --source` and `--quota`** name the orchestrator's subscription and read quota readings from a file, so the footer can be checked without a live poll.

### Changed

- **A sub-agent's rules come from its own brief and its own language**, not from the orchestrator's. ts-dev gets the TypeScript rules even when the brief names no `.ts` path. The orchestrator no longer carries them on a task that is not TypeScript.
- **A sub-agent's named references are in its prompt**, within a size limit that names anything it cuts.
- **The footer shows every subscription in use**, the orchestrator's and each running sub-agent's. Below 150 columns it shortens them to `claude 62%  |  codex 40%` rather than hide one.

## 0.5.0-rc-fix4 - 2026-09-26

Sessions become one folder of two files, tofu's own state moves under the home, and background shells work on Windows.

### Added

- **Settings, Models & roles, assigns a model to the orchestrator and to each sub-agent.** Each row shows where the definition came from. The picker offers `none (disabled)` and `inherit`, and writes `.tofu/agent-models.yaml` or `~/.tofu/agent-models.yaml`, as the scope tab says. A sub-agents card names the definition and the model it runs.
- **The library ships a `ts-dev` sub-agent**, seven general coding rules (read first, minimal diff, boundaries, evidence, and on a trigger debug, review and refactor), eleven TypeScript rules, and three TypeScript references.
- **`tofu migrate`** moves an old project `.tofu` into the home folder and converts old records into sessions. `--dry-run` shows the plan first.
- **`tofu session trace`** follows a session's calls, sub-agents and results by id.
- **The setting `instructionSources`** orders AGENTS.md and CLAUDE.md.

### Changed

- **A session is one folder holding `session.json` and `events.jsonl`.** Every turn and every sub-agent run is in that one log, and each message is written once. Images live beside it in `attachments/`. Old records still read, and `tofu migrate` converts them.
- **Tofu's own state lives under `~/.tofu/projects/<project>`.** A project's `.tofu` holds only what you write: agents, agent-models.yaml, rules and settings.
- **In a folder with both AGENTS.md and CLAUDE.md, only AGENTS.md is sent**, and a notice names the skipped file.
- **A background start waits up to 10 seconds.** A command that ends in that time returns its output and leaves no shell entry. Only a process that keeps running is listed, and exited shells are removed at the next launch.

### Fixed

- **Background shells run in Git Bash on Windows**, the same shell as every other command. They ran in WSL before, which could not run Windows `npm`.
- **The settings inspector fits its pane**, and it scrolls.

## 0.5.0-rc-fix3 - 2026-09-26

Named sub-agents, each on its own model, and the fixes from the first drive of rc-fix2.

### Added

- **`tofu agents` lists the named sub-agents** from `.tofu/agents`, `.agents/agents`, `.claude/agents`, the same folders under the home, and the library. For each it shows the model, the folder it came from, and the tools. The first definition found under a name wins. Claude's model aliases (`sonnet`) and tool names (`Read`, `Grep`) are translated, and a tool tofu does not have is listed as ignored.
- **A turn spawns a named sub-agent on that sub-agent's own model.** `.tofu/agent-models.yaml` assigns a model per sub-agent, and `none` disables one. The orchestrator is told which sub-agents it may name.
- **The setting `agentSources`** chooses which of `tofu`, `agents` and `claude` are read.
- **A number setting opens a dialog when you press Enter or click it.** The dialog shows the current value, the default and what it means, and the range. A value outside the range is refused there, by `+` and `-`, and by `tofu settings set`.

### Changed

- **The roles read `orchestrator` and `(unnamed sub-agent)`**, in settings, the picker and `tofu models`. An old `roles/turn.yaml` still loads.
- **The Commands list leads with each command's name**, and a long description is cut to one line.
- **Sub-agents, file edits and shells place their scrollbar as chat does**: in the last column, with one empty row above the footer.
- **The footer shows only the subscription the orchestrator is using**, and shortens its label before it drops the percentage.
- **The composer hint says Shift+Enter** inserts a line break.

### Fixed

- **`cooked for` appears once**, in the line above the composer.
- **An empty composer shows one cursor.**

## 0.5.0-rc-fix2 - 2026-09-26

`tofu --continue` opens on the conversation it continues.

### Fixed

- **`tofu --continue` shows the session it resumes.** Chat holds each task and answer, tools fold into their one line with `[tool#id]`, and the top row names the session. Before, chat opened empty.
- **A resumed session sends each earlier turn once.** The first turn used to reach the model twice.
- **The first turn after a resume no longer prints every carried tool result as a note.**

## 0.5.0-rc-fix1 - 2026-09-26

The new interface: one top row, four screens, a settings screen where every row does what it says, and a cover with a cat.

### Added

- **One top row and four screens.** The top row holds chat, sub-agents, file edits and shells. Tab cycles them, and `[settings]` in the top row opens settings.
- **`tofu` opens a new session on the cover**, a pixel cat above the wordmark and a composer; `tofu --continue` goes straight to chat.
- **Settings is a screen.** Each category is a row list with a live preview. Enter changes a value, arrows preview a choice and Esc restores it, and Ctrl+K searches every setting. Theme offers eight palettes, and `terminal` strips every colour.
- **Ctrl+K searches screens, agents and events; Alt+K lists commands; `/` opens the command menu; `@` opens a file picker** that stays inside the workspace.
- **References are links.** `[edit#id]` opens its diff, `[&agent]` selects the agent in sub-agents, and only the reference's own cells are clickable.
- **The mouse selects inside one pane.** A drag copies without the card's border or padding, Alt+click quotes into the composer, Shift+click and Ctrl+Alt+click collect, and Ctrl+R quotes the selection.
- **A paste of 160 characters or more becomes `[Text N characters]`** in the composer and is sent whole.
- **Shift+Tab cycles reasoning effort** on a model that offers levels, and the footer shows it.
- **Shells show PID, working directory and owner**, and killing one asks first.
- **codex-sub models take a pasted image**, and a history entry recalled with an image sends it again.
- **`tofu drive` takes `paste`, `absent` and `images` steps**, and `tofu frame --list` names 99 frames across every screen and dialog.

### Changed

- **Tools stay one line each in chat**, in the new theme, and markdown still renders.
- **The session name is whole whenever it fits** beside the tabs; the git branch gives way first.
- **ASCII mode replaces every glyph with one character**, so no row grows past the terminal.

### Removed

- The old views: the picker, the work screen, and the separate links and quote screens. `/links` and `/quote` are now dialogs.

## 0.4.19 - 2026-09-25

A turn of more than one step finishes again, and a sub-agent hands back a contract and stays inside what it owns.

### Added

- **A sub-agent hands back a typed contract.** A child ends its answer with a `claims` list, each line carrying the command it ran, what that printed and whether it was met, and a `blocked` list for what it could not do.

- **A sub-agent is refused a tree-wide command.** A child that runs `gofmt -l .` is told the parent runs it, and reports that rather than working around it.

- **`tofu models` says what kind each row is and what pays for it**, `claude-sub/claude-opus-5 (kind llm, pays subscription)`, so the same model reached two ways reads as two different bills.

### Fixed

- **`tofu run` finishes a turn of more than one step.** A reply carrying a thinking signature and no thinking text was sent back with the text field missing, and Anthropic refused the next request.

## 0.4.18 - 2026-09-25

The library follows the directory you point tofu at, and the screen stops committing markdown it has not finished reading.

### Added

- **`fetch` can be turned off from the library.** `library/web/fetch.yaml` carries `use: on`, a byte cap and a timeout, so a project that should not reach the network says so in its library rather than in an argument nobody remembers to pass.

### Fixed

- **`--dir` decides every layered library, including the fifth.** Four of the five layered libraries read the directory you named and one read the working directory, so a rule or a question could come from the project you were standing in rather than the project you pointed at. The web library was the one that disagreed.

- **A sift rule declaring `shadow` no longer cuts.** `shadow` means watch and report, and the shell sift read the rule's mode and then cut anyway, which is the one thing a shadow rule must not do. It was passing `ModeEnforced` at the call rather than the mode the rule declared.

- **A table in a streamed reply keeps its columns.** The screen commits the part of a message it considers finished and redraws the rest. A table growing a row at a time could be committed mid-table, which freezes the rows already drawn at their old width and leaves the next row as literal pipes below them.

- **A code block inside a list no longer splits the list in two.** The same cut, made when a fence closed while its list item was still open. The list ended at the fence and the items after it started a new one, with the spacing restarting in the middle.

- **The interface cuts a long shell result the way `tofu run` does.** One of the two carried the sift and the other did not, so the same command filled the screen in the app and was trimmed at the command line.

- **The shipped library travels with the binary.** It was read from the source tree, so a binary run anywhere else fell back to whatever it could find.

- **`bench corpus` reads its own imports and no longer skips itself**, and a failed row in a sweep fits the table it is printed in instead of running past the columns.

## 0.4.17 - 2026-09-24

The screen stopped disagreeing with the record, and then stopped disagreeing with itself.

### Added

- **`tofu drive` takes `--home` and prints the settings it resolved.** Without one it makes an empty home for the run and deletes it after, so every setting is its declared default and a driven screen no longer depends on a settings file the script never named. **Three times a stray file changed a driven answer and twice it made a check pass that should have failed.**

- **`bench corpus` reads your recorded sessions without spending anything.** Session count, when each was recorded, its shape, its steps and messages and reads, and which of them carry an event kind this build does not read. Before this, the only path that read a recorded session went through an arm that pays.

- **A driven script can hold a reply open.** A recorded reply marked `"unfinished"` streams its text and then waits, the way a model still generating does, which is how a check reaches the middle of a turn rather than only its end.

### Changed

- **One `ctrl+c` stops the agent, not everything, and a second always means a second.** With a tool still running, the first press ends the model request and lets the open calls finish. **A turn running a sub-agent now stops on the first press**, because a child is a turn and letting it finish is the opposite of what the press means. The window that used to require the two presses within three seconds is gone from stopping and kept for quitting.

- **A running sub-agent's clock advances.** It was frozen when the event was built, so it moved once per model call and stuck inside one, which on a real wire is most of a child's life.

- **A sub-agent speaks in its own panel rather than in the parent's column.** Before, a child's messages were drawn into the transcript with nothing saying who said them, and its last message could be joined to the parent's first with no break between the two.

- **A timeout above the cap runs at the cap and the result says so.** It was refused, which ended the whole sub-agent that asked, over a number one order of magnitude out. Zero still means the default and a negative still runs at the default, because neither is the same mistake.

- **A background process stops when tofu exits, and the line on the way out names what stopped.** It survives its turn as before.

### Fixed

- **A stopped turn keeps the tool rows it produced.** They were folded away at the default setting, so a turn you interrupted showed nothing of what it had run.

- **A tool call that finished after you stopped the turn shows its result.** The result was only announced when the next model request was assembled, and after a stop there is no next request, so the screen said `no result` while the session file held it.

- **A sub-agent that was stopped reports what it did.** It handed its parent a cancellation instead of its work, and the report called it an error rather than a stop, which are different instructions to the model that reads it.

- **The command in the ledger is the command that ran.** One reader trimmed a tool's name off the front, so `bash script.sh` was recorded as `script.sh`, a command nobody ran. Seventeen places set that field and four of them disagreed about what it meant.

- **A missing judge key reads as a state rather than a failure.** The first line of every session without one was a log message carrying a function name and an absolute path; it now says what to do, and the raw error moves to the work view.

- **A fork leaves a line in the transcript.** The notice existed for two frames and nothing could read it.

- **Columns keep their gap and clocks line up.** A truncated value ran into the text beside it, and a clock wider than its column pushed every row after it out of line.

## 0.4.16 - 2026-09-24

The interface stops lying about what it is doing, and tofu can now be driven and read by something other than a person.

### Added

- **`tofu drive` runs the interface with no terminal and no model call.** It takes a script of what a person does, `type`, `key`, `wait`, `screen` and `environment`, drives the real app, and prints the screen it was asked for. The model is a cassette named by `--cassette` or `TOFU_DRIVE_CASSETTE`, one recorded reply per line of json. **Without a cassette no wire opens at all**, so a driven run cannot reach the network by construction. A `wait` that never arrives exits 1 and prints both what it wanted and the screen it got.

- **`bench corpus` reads your recorded sessions and prints what is in them, spending nothing.** Session count, when each was recorded, its shape, its steps and messages and reads, and which of them carry an event kind this build does not read. Before this, the only path that read a recorded session went through a spending arm.

### Changed

- **One `ctrl+c` stops the agent, not everything.** With a tool still running, the first press ends the model request and lets the open calls finish, and the screen says so. A second press within three seconds stops them too. **Whatever you typed while waiting is kept**, and the note tells you how many messages the queue still holds. At an idle prompt, the first press asks before it quits, and typing anything cancels the question.

- **A session recorded by a newer build still reads.** An event kind this build does not know is skipped rather than refusing the whole file. Every reader that used to refuse one now goes through the same reading, and `tofu context` and the bench corpus stopped disagreeing about what a session is.

### Fixed

- **The interface resolves its shell the way `tofu run` does.** Everything 0.4.15 promised about the shell was true in `tofu run` and false in the app, because the two built their tools through different callers and only one was wired. On this machine the app got WSL and `npm` failed with `Permission denied` at exit 126. The app now reads the `shell` setting, resolves once, and shares that resolution with the environment block.

- **A session recorded since 0.4.15 could not be read back.** `tofu context` exited 1 with `event 1 is of unknown kind "prompt"`, and the bench corpus reader refused the whole session, both because a kind was added and the readers were not told.

- **A resumed session keeps its tool calls and its reasoning.** The reading that replaced four separate readers modelled a message with fewer fields than the turn writes, which would have dropped tool calls, thinking and reasoning items on the way back in.

- **The sub-agents panel no longer shows a child a step behind.** A finished child step was recorded on its own goroutine, so the screen could be drawn before the roster had been told what the child did.

## 0.4.15 - 2026-09-24

Tofu runs in a shell that can see your project, and stops throwing away what the model already thought.

### Added

- **When you answer a permission prompt, your answer is written onto that decision's row** in the log tofu already keeps, marked `gate-answer` so it is distinguishable from one typed by hand. Nothing else about the prompt changes. A decision you were never asked about stays unmarked, and so does one allowed by a rule you granted earlier, because neither is a fresh judgment. **This is how a threshold eventually gets calibrated against your own use rather than a number somebody typed**: 1.37 percent of gate decisions ask, so it accumulates over weeks of ordinary use rather than days.

- **The prompt now tells the model which shell it has and what that shell can run.** Two lines: the shell and its family, and the project's interpreters with their versions or the word missing, chosen from what the project declares, `go.mod` for go, `package.json` for node and its package manager, `pyproject.toml` for python. Probed once per session with a two second limit each, never per turn. **Absence is the half that matters**: a shell that cannot see `node` now says so before the first step instead of after thirteen.

- **A command naming an interpreter that is not there is refused before it runs**, saying which one is missing and what the shell does have. Before, it spawned and came back with whatever the shell prints, usually `command not found` and an exit code, which a model has to interpret one command at a time.

- **`TOFU_SHELL` picks the shell.** A path uses that shell. The word `wsl` uses WSL on purpose, with a line in the prompt saying its filesystem is separate and its PATH will not see software installed only on Windows.

### Changed

- **On Windows, tofu finds Git Bash through `git` rather than through PATH, and refuses WSL unless you asked for it.** On a machine with Git for Windows, `bash` on PATH is usually `C:\Windows\system32\bash.exe`, which is the WSL launcher: a separate filesystem with its own PATH, where the node you installed on Windows does not exist. **That is a real session: eighteen steps, thirteen of them hunting for a runtime that was reachable the whole time from the shell tofu did not pick.** If no posix shell resolves at all, PowerShell is used and the tool description says the posix syntax no longer applies.

- **The bash tool's description no longer carries your machine's path**, so the tool block is byte identical in every directory and can be cached once rather than per project. It was 16,574 bytes in one directory and 16,575 in another, for a one character difference in an embedded path.

- **A recorded session carries what the model was thinking, and hands it back.** Anthropic requires a thinking block to be returned inside a tool-use turn, and tofu was dropping it: a session file now carries `thinking` and `thinking_signature`, and a codex reasoning item its own `reasoning` object with an id and its encrypted content. **A session recorded before any of this still loads**, including one written before the field existed at all.

### Fixed

- **`--done-review` with `turnMaySpawn` off is refused, and the refusal names the setting.** Before, it quietly turned sub-agents off and ran an arm whose children are never reviewed, which says a check is running that is not. Exit code 2, and the message names the setting rather than a flag you did not type.

## 0.4.14 - 2026-09-23

They are sub-agents, and you decide whether a turn may spawn one or edit a file it has not read.

### Changed

- **`--no-crew` is now `--no-subagents`.** They are sub-agents everywhere: in the interface, in the settings, in the verbs and in what tofu writes to you. A script passing the old flag stops working and says the flag is unknown.

### Added

- **A setting decides whether a turn may spawn a sub-agent**, `turnMaySpawn`, on by default, in the settings pane under `turn`. It applies to a turn started in the app and to `tofu run` alike. **`--no-subagents` turns spawning off for one run whatever the setting says, and there is no flag that turns it on when the setting says off**, so the setting cannot be bypassed from the command line it is meant to constrain.
- **An edit or a write to a file the turn has not read is refused**, `readBeforeEdit`, on by default. The refusal carries the file's current content, so the next attempt is informed rather than another guess. A read earlier in the turn counts, and so does an edit or a write the turn made itself: **a search does not, because it shows the matching lines and cannot vouch for the rest of the file an anchor edit is aimed at.** Measured against 110 recorded turns before it was turned on: of 55 real edits, one would have been refused.

### Fixed

- **Tofu carried this project's own development notes into every repository it ran in.** Eighteen files under the shipped library held ticket numbers, benchmark paths and dated report citations, all of it embedded in the binary and none of it usable by a turn. The rules themselves are unchanged: what went is the bookkeeping around them.

## 0.4.13 - 2026-09-23

A file tofu reads cannot crash it, and you can tell it to ignore the house rules.

### Added

- **`tofu rules list` shows a mode only where the mode decides something.** A threshold rule's mode is read and is shown; a rule with no checker and no off setting had a mode printed beside it that nothing consulted, which read as a setting you could change.

### Fixed

- **A ledger file, a subscription filename, a credential row, a cached answer and a zero valued message role could each crash tofu rather than being refused.** All five were reachable from a file or a reply rather than from code, none had ever been reached: 2,780 recorded decision rows, 3 credential rows, 75 cached answers and two subscription files were checked and every value was one this build knows. Each is now refused where it is read, and the row that fails says why instead of disappearing.
- **A credential whose provider tofu cannot read no longer disappears from the list.** It stays, marked disabled with the reason, so an account you cannot use reads differently from an account you never had.

### Added

- **`tofu run --no-instructions` sends no instruction file at all**: neither the nearest `AGENTS.md` or `CLAUDE.md` at or above the working directory, nor your personal `AGENTS.md` or `CLAUDE.md` in your home directory. The default is unchanged and still sends both. Until now a run inside a repository carrying instructions written for another tool had no way to say no, and the benchmark arranged it by writing empty files into the tree. `--show-prompt` says `instruction files: off by request` under the flag, so a missing block reads as a choice rather than an empty directory.

## 0.4.12 - 2026-09-23

You choose what runs, and you can point at what was said.

### Added

- **`tofu run --effort <level>`** sets how hard the model thinks: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`. The vocabulary is the vendors' own, not a new one. The anthropic wire takes `low` through `max` and refuses `minimal` by name rather than picking a neighbour; the codex wire takes all seven. The openrouter wire sends no level, so `--effort` with `--wire key` is refused instead of being dropped.
- **A `[quote#abcd]` reference resolves to the turn it names.** `/quote` has inserted one since 0.4.9 and nothing could read it back. A tool now returns that turn's words, so the model cites rather than paraphrases. An id matching two turns, and an id matching none, each come back with their own message asking for the reference again rather than quoting the nearest turn. A turn recorded before event ids existed is quotable: 856 of them are.
- **The crew view shows what a child is calling while it is calling it.** It carries when the child started, when it last stepped, how many steps it has taken, and the names of its last five calls. It said "no tool call yet" for the whole life of a running child before.

### Fixed

- **`tofu shell kill` returned while the tree was still running.** Terminating a job is asynchronous and nothing waited on its members, so for about 16 milliseconds after kill reported success the children still held the port and the log. An immediate restart under the same name hit "address already in use".
- **A process the shell spawned before it joined its job escaped kill for its whole life.** The gap was about a third of a millisecond against a first child at 39, so it never fired on an idle machine. Held open on a real condition it fires every time: 40 of 40 rounds, 20 of them still running when kill returned. The shell is created suspended and resumed only once it is a member, so nothing it spawns can predate its membership.
- **The crew view kept its own copy of the crew, with its own clock.** The elapsed time a person read was measured from a timestamp the view took, not from when the child started, and three of its six states could never appear.
- **The model picker offered a model a hook forbids.** Its default selection was on one of the two banned models, and nothing checked the excluded flag at the point of picking.
- **Eleven shipped rules declared a mode that could not do anything**, and one of them said `enforced`. A mode only chooses for a rule that names a checker. Three rules carry no checker and no longer carry a mode, and `enforced` is refused by name on the kinds that cannot have one.

### Changed

- **A turn now thinks at `medium` by default.** It ran at none until now, which nobody chose: it was what an unset field did. Every subscription turn, in the terminal interface as well as on the command line, now carries a thinking level, and a turn costs more output tokens than it did.
- **The model picker changes the model, not only the subscription**, and carries the thinking effort beside it, chosen with the left and right keys, because the model and the effort are one choice. It offers only the levels the picked subscription's wire accepts. The pick lasts until the app closes.
- **The anthropic request carries `output_config.effort`** and the `effort-2025-11-24` beta with it. The request field was a bool that nothing ever set and is now a level.

### Note

**The benchmark was understating tofu's own token use by 62 percent.** The tofu arm counted prompt plus completion and ignored cache entirely, while the claude arm counted all three into one column, so the two sat in the same table under two definitions of billed input. Every tofu against claude token figure produced before today was wrong in tofu's favour.

**The three benchmark arms are asked the same thing now.** All three run at medium, and the runner says so on its own last line.

**Three corpora leak and none was repaired.** In one, three of four questions name their own answer inside their own prompt, so a regular expression scores three of four: that package is parked, because the recorded sessions yield seven usable turns carrying two distinct tasks against a floor of thirty. A planted state in another is recoverable from a substring. Each is named in its own provenance rather than quietly fixed, because the leak is the evidence that a figure was wrong.

**Nothing on the report page cites a figure a withdrawal struck.** The build refuses it, and a quotation that is a table row strikes every row of that table.


### Added

- **`tofu run --effort <level>`** sets how hard the model thinks: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`. The vocabulary is the vendors' own, not a new one. The anthropic wire takes `low` through `max` and refuses `minimal` by name rather than picking a neighbour; the codex wire takes all seven. The openrouter wire sends no level, so `--effort` with `--wire key` is refused instead of being dropped.

### Changed

- **The model picker changes the model, not only the subscription.** Picking `claude-sub/claude-sonnet-5` runs the next turn on that model, through the same check `tofu run --model` goes through, so an excluded model and a model on a subscription the turn cannot reach are still refused. The pick lasts until the app closes.
- **The picker carries the thinking effort beside the model**, chosen with the left and right keys, because the model and the effort are one choice. It offers only the levels the picked subscription's wire accepts, and an effort the other subscription refuses falls back to `medium` when the pick moves there.
- **A turn now thinks at `medium` by default.** It ran at none until now, which nobody chose: it was what an unset field did. Every subscription turn, in the terminal interface as well as on the command line, now carries a thinking level, and a turn costs more output tokens than it did.
- **The anthropic request carries `output_config.effort`** and the `effort-2025-11-24` beta with it. The request field was a bool that nothing ever set and is now a level.

## 0.4.11 - 2026-09-23

Tofu acts on what it measured. A bash result arrives cut, and one file says what decides what.

### Added

- **A bash result reaches the model cut**, with a line at the end saying how many of how many bytes went and which method decided. Over 170 recorded bash results in 35 sessions the free arm saves 49.1 percent of bytes, concentrated in a few large outputs: the median session saves nothing and the best saves 74.7 percent.
- **`tofu run --sift <arm>`** picks the arm by hand, `free` or `judged`, where the method table otherwise decides. `--help` prints what each arm costs, read from the table at print time.
- **`library/decisions/methods@1.yaml` names the method for every decision point**, one line each, reading judged, cheap or unwired. Changing which method decides a point is changing one line. **Unwired is a stated answer with a reason, not a default.**
- **`tofu rules fired [date]`** reads back the record tofu already writes on every rule that fires. 223 fires were on disk with no way to look at them.
- **`tofu usage --history`** shows every recorded quota reading with the moment it was captured. 28 were on disk and only a benchmark read them.
- **`/models` opens the model picker**, and the pick changes the wire the next turn runs on. It offers only the subscriptions the turn can actually run, and an excluded model cannot be picked.
- **`edit` takes a Go symbol**, so replacing a function no longer means reproducing the old one character for character. Any edit that would leave a Go file unable to parse is refused with nothing written.
- **A decision row says whether a turn or a benchmark wrote it.** 1,104 of 2,756 recorded rows are bench measurements, which is 40 percent, and nothing could tell them apart before.

### Fixed

- **A fresh clone did not build.** The method table and its parser were never committed, and `library/embed.go` embeds the library.
- **Two agents could be given the same path.** The collision check and the permission check were two implementations of one glob language and disagreed on whether a subtree glob covers the directory it names. There is one matcher now, and it narrows: more spawns are refused, none newly allowed.
- **The model picker offered a model a hook forbids.** Its default selection was on one of the two banned models, and nothing checked the excluded flag at the point of picking.
- **`tofu reload` named two loaders that do not exist.** It said it had skipped skills and hooks. There is no loader for either, so it says what it actually re-read.
- **`frame` was a verb the usage text never mentioned.** A test now compares the usage text against the verb table.
- **The benchmark understated tofu's own token use by 62 percent.** The tofu arm counted prompt plus completion and ignored cache entirely, while the claude arm counted all three into the same column, so the two sat in one table under two definitions of billed input. Every tofu against claude token figure produced before today was wrong in tofu's favour.
- **The benchmark measured repeat two on top of repeat one.** The runner never staged the task's seed between repeats.

### Changed

- **The three benchmark arms are asked the same thing.** Claude runs at `--effort medium` and codex at `model_reasoning_effort=medium`. **Tofu has no effort flag at all and its request leaves thinking unset**, so it runs at none, and every row says so rather than leaving a reader to find out.
- **The report page reads what decides what.** It said 0 of 8, hand written; it says 3 of 9 now, derived from the method table, and the build fails if the page and the table disagree.
- **The link-only rule lives once.** It existed twice, character for character, in the fetch path and in the sieve. The benchmark was measuring the second copy and reporting what the first already does.

### Note

**`page_sift` is measured and not wired, and that is a result rather than a gap.** Its cheap arm elides 0 further bytes on all 24 recorded pages, because `web.Reduce` already strips the same rows at fetch time, where `Reduce` itself takes 11.69 percent. A second pass of one rule is not a decision point.

**The shell sieve acts with no calibration lock, so `tofu doctor` reports shadow while a turn cuts.** The rule's threshold is a prior and not a fit. The two will be made to agree.


## 0.4.10 - 2026-09-22

Tofu tells the model which rules apply to the work, and can show you exactly what it sent.

### Added

- **The system prompt is composed from the rules that fire for the task.** A rule declares which of ten concerns it is, five of which are always on, and the other five fire on the language of the paths, the directories touched or the verb the task names. A rule that fires and carries no text fails the run rather than being dropped quietly.
- **`tofu run --show-prompt` prints the prompt a turn would use** before it runs: every part, its concern, its size in bytes, and the rule file it came from, then every held-back rule with the reason it did not fire.
- **`tofu rules index "<task>" [paths...]`** answers the same question without running anything.
- **`tofu why <id>` prints the state a decision was made on**, reading back a state that was written to its own file. A missing state file, a state file outside the ledger, and a state that is there now read differently from each other.
- **`projectInstructionsCap` is a setting**, 32 KB by default, and **tofu tells you on screen when your instruction files are cut** rather than only telling the model.

### Fixed

- **Your instruction files were cut at 16 KB and nothing said so on screen.** The cap is 32 KB now, which is what the one other harness that caps this uses, and a 17 KB `CLAUDE.md` reaches the model whole.
- **Every rule cited a source under a directory that is not in the repository.** Thirteen citations pointed at a scratch path on one machine, so a fresh checkout had thirteen dead references. Every rule now cites a file under `library/`, and a test refuses one that does not.
- **The composed prompt carried the absolute path of every rule file on the machine it ran on.** It names the rule now, and `--show-prompt` still prints the path for a person debugging.

### Changed

- **A rule carries the text a model reads, separately from the notes a maintainer reads.** Before this, nine of ten rules had only maintainer notes, so a prompt told a model that a rule "wraps internal/crew.Matches" rather than telling it not to write outside its paths.
- **`bench/report/index.html` answers three questions**: is tofu better than claude and codex, is this version better than the last, and where does a judgment beat the cheaper way. The first two say what they would need rather than filling a table from one sample.

### Note

**Nothing tofu judges changes what tofu does.** All nineteen shipped rules and every decision point read `mode: shadow`. The report page says so on its own front tab: 0 of 8 measured decisions are switched on.

**Of the six decisions with a cheaper method measured beside them, Jev wins four and loses two.** `shell_sift` is the clearest win at 1.50x and switching it on would cost a median $0.00054 a session, one cent in the worst recorded session, and 838 ms on a shell tool call.

**rtk was measured for the first time.** In front of Jev it buys a 29 percent cheaper call and pays 5 of 34 needles for it. Neither rtk arm beats Jev alone, because rtk has no filter for 21 of the 34 recorded commands.

## 0.4.9 - 2026-09-22

Tofu runs on the account that has room, and every number it reports is one you can check.

### Added

- **Tofu picks an account instead of refusing.** With more than one account on a subscription it chooses once, at session start, by how much headroom is left across the windows that bind, and it stays on that account for the session. `tofu login --disable` is no longer the way to get past a second login.
- **An account that runs out mid-session moves rather than stopping.** The session forks onto the next account carrying its handles, and the screen says which account it moved to, why, and what the move cost. A child picks its own account and leaves the parent's drained one alone.
- **`tofu rules index "<task>" [paths...]`** says which rules would fire for a task and why each held-back rule did not. A scope that reached nothing reads differently from a condition that did not match, and a task naming no paths is answered rather than refused.
- **`/links` lists every link the conversation carried**, newest first, with a count when one appeared more than once. Only `http` and `https` are shown, and a link with credential-shaped query parameters is shown with the parameters cut.
- **`/quote` inserts a reference to a past turn**, `[quote#39cl]`, and nothing else. The reference is an id into the record rather than pasted text, so it cannot go stale and does not cost its tokens twice.
- **Every quota reading is written to `.tofu/quota/<date>.jsonl`** when tofu polls a vendor, carrying the credential row number, the fetch time and each window's id, use and reset. No token, account id or email is ever in a row.
- **`bench/report/index.html` is one page over every dated report**, with the headline number, the arms, the sample size and the skips, and it opens from disk with no network.

### Fixed

- **An account with room read as spent.** A vendor reports per-model windows beside the account windows, and one of those sitting at its cap made the whole account unusable. A window binds only when it is not scoped to a model, so an account with most of its five-hour window free is usable and is ranked properly.
- **The token estimate read 32 percent over on Codex.** One constant served two wires that tokenize differently. Measured over the recorded corpus, the median error across both wires falls from 18 percent to 7 and the worst from 47 to 20.
- **A recorded step's occupancy described a request nobody sent**, because it was measured after that step's tool results were appended. It is now the request as sent, and a recorded row keeps every band: the largest one was silently decoding as zero.
- **The context bar showed the number the fork decided on** rather than what the request occupied. They are different quantities and the bar now shows the one a person watching a turn can act on.
- **A file path read back out of the ledger, and a session handle typed on the command line, were both followed without a guard.** A path that leaves its root is refused, and the refusal cannot be mistaken for a missing file.
- **A truncated tool result said only that something was missing.** It now says how many bytes were dropped, and when the whole output could not be stored it says that too, rather than leaving a hole with no explanation.

### Changed

- **A rule declares which of ten concerns it is, and the declaration is required.** Five concerns may never be conditional, so a rule that is about output shape, safety, the environment, tool guidance or the report format is refused if it declares a trigger at all. Two shipped rules were firing on the wrong trigger and one of them fired on a task naming only `CHANGELOG.md`.
- **A rule fires on the language of the paths, the directories touched, or the verb the task names**, rather than on a path glob standing in for all three.
- **The harness reports a median and a range over repeats rather than a mean.** A comparison where either arm has fewer than two passing repeats prints `not separable`, and a task that passes a gate on one repeat and fails on another is named unstable and ranks no arm.

### Note

**Three measurements in this release returned no, and that is the point of having them.** A moved cache breakpoint caches exactly as much as the old one, because the vendor looks back twenty content blocks and a turn here adds at most eight. The recorded corpus cannot separate a thrift arm from a shaping arm: six of 109 sessions carry both, and 22 sessions are one fixture written to produce reading. And the account picker cannot be scored against any arm yet, because until this release nothing ever wrote a quota reading down.

**None of the 25 reports under `bench/` is built by rerunning its own code.** Every one is built from the text of its own markdown, and every one now says so on its page.

**A recorded session written before this release keeps the old meaning of its occupancy** under the same key, and nothing in a row tells the two apart. Sessions recorded before event ids existed get a derived id when quoted, which resolves the same way every time but is not the id the record carried.

## 0.4.8 - 2026-09-22

`catalog` is `library`.

### Changed

- **The `catalog` directory is now `library`, and `tofu catalog` is now `tofu library`.** Two other tools already call the same thing a catalog, and a shared word made this look like a copy of theirs when the contents are ours. `library` also says the right thing: a place things are looked up from, rather than a list of what exists.
- `tofu rules list` and `tofu rules check` take `--library` where they took `--catalog`.

### Deprecated

- **`tofu catalog` still runs and tells you the new name.** It goes at the next minor bump.

### Note

A decision recorded before today names its rule file by a `catalog/` path in the sentence `tofu why` prints. **Nothing resolves a path from that sentence**, so old decisions replay unchanged; the only effect is that `tofu why` on a row from before this release names a directory that no longer exists.

## 0.4.7 - 2026-09-22

More than one account, and a screen that stops lying about which one.

### Added

- **`tofu login --status` is a listing rather than a line.** Each subscription is a heading, each account under it carries the email it belongs to, the plan when the vendor reports one, and how full each quota window is. `--redact` masks the account when you are sharing a screen.
- **A second account for the same subscription is stored and reachable.** Signing in again no longer overwrites the first one. When two are usable and nothing says which, tofu refuses and names both by number rather than picking by accident, and the listing says so with the command that sets one aside.

### Fixed

- **A second Codex login used to overwrite the first.** A Codex credential stored without naming its account, so both rows collided. A login names its account now, and the row already on disk is repaired the first time tofu opens the store.
- **With two accounts on one subscription, quota was polled for neither.** Each account is polled for its own windows, and a window is remembered against the account it belongs to rather than against the vendor.
- **`tofu usage` said no credential was stored while listing two of them**, whenever every window was spent.
- **The account was printed in full in the settings pane and masked in the listing.** One rule now, and the mask tells two accounts apart instead of rendering them identically.
- **A command that succeeds and prints nothing is no longer sent as an empty message**, which some vendors refuse. An empty success and an empty failure also read differently now, so the model can tell a command that worked from one that broke.
- **A turn that stops itself keeps its last word.** Tool calls left unanswered when the loop guard tripped made the final request malformed, so the explanation of why the turn stopped was the thing that got lost.

### Changed

- The token benchmark replays against a fixed commit instead of the working tree, so its numbers measure the cap rather than yesterday's edits.

## 0.4.6 - 2026-09-21

A rule file with one bad number no longer turns the gate off, a failed request is tried again, and the mouse works.

### Fixed

- **A tool gate rule that cannot be satisfied now refuses to start a turn instead of running every call unjudged.** A missing key and an unusable rule were one branch, and both left the gate off behind the same banner. They are two now and they read differently on screen. **There was also no range check at all**, so a threshold of 1.85 on a zero to one scale loaded cleanly and asked the model anyway.
- **Both subscription wires retry.** One overloaded answer used to end a turn five steps in. The retry window is the connection, never the stream, so a retried request can never repeat text already on your screen. **A step can now put five requests on the wire, and a retried one may still bill.**
- **A one letter answer no longer approves a gate ask.** The answers are `1`, `2` and `3`. Typing a sentence that began with `a` under an open ask used to allow the call on its first character.
- A pale column down the left of the composer. It was a border glyph painted over the tint, and the tint is meant to be the only edge.
- An id typed from the transcript reaches the work view. It was matched against the start of an id where the screen draws the end.

### Added

- **The mouse.** The five tabs and any trace id are clickable. Everything else on the screen still selects with a drag, and a drag that starts on a tab selects instead of switching.
- **A gate ask says what tripped it in words**, using the wording the judge itself was given rather than a scale invented here. A decision recorded before this still prints its numbers.
- **A command proxy, off by default**, set in `catalog/tools/shell/proxy.yaml`. It rewrites a shell command before the gate sees it, so the ledger records the command that actually ran. A project's own filter file is refused, and a proxy that crashes falls back to the raw command.
- **`README.md`.** The repository now says what tofu is in its first line, that it is personal and not released, which judgments lost to their free arm and are switched off with the numbers that say so, how to build and drive the binary from a fresh checkout, and where in `bench/` the numbers live. There is still no `LICENSE`, so the default is all rights reserved.

### Measured

- A pasted screenshot costs about 1,765 tokens. 190,476 bytes at 1536 by 850, measured against the same turn without it and cross-checked against the vendor's own area rule. **File size says almost nothing about the cost.**
- A recorded session runs a median of 7 tool calls and at most 55. 1.5 percent of tool result bytes come off losslessly, and `glob` is 92 percent of the corpus by size with none removable.
- Fifty three production failures published by another harness were checked against this tree: five were real and are named in `.local/boji/planning/doing/known-failures.md`.

### Removed

- **The `grep` tool is gone. `search` is the only way to find text.** Over a tree of 13,140 files the two tools took 48.5 s and 37 s for the same query, and returned 133 MB against 17 KB. **The removal is about the bytes, not the seconds:** a tool that hands back 133 MB spends a context window on one call. `grep` could not be capped, because every cap tried cost half the correct answers on a thirty question bench, so it goes rather than shrinks. `search` now says in its own description that it finds text and that there is no grep tool, so a model does not reach for what is not there.

### Changed

- **`tofu search` reports what it scanned.** A call says how many files it looked at and how many it returned, and stops after 1,200 candidates rather than reading a whole tree. The cap has never fired on a real question.

## 0.4.5 - 2026-09-21

What is happening right now has its own line, and five things on screen have their own colour.

### Added

- **A progress line under the running summary.** The summary carries the counters and the line beneath it carries the one call happening now, with a spinner while it runs. It replaces itself, so a turn with twelve tool calls is still two lines. **The sub-agents view draws the same line for a child.**
- Five things that were all the same dim now read as themselves: a tool call, a shell command, a file path, an event id, and a finished call.
- Whether the running line counts shell calls separately is a setting.

### Changed

- **The separator above the composer is gone.** The tint already says where the input begins and two marks for one boundary is one too many.
- **The composer has a blank row above and below its text, inside the tint**, so it reads as an area rather than a strip with words in it.

### Fixed

- **A pasted image deleted from the message was still attached.** Two separate paths carried it: the chip list under the composer, and a list of files on disk that never looked at the message at all. **Both now keep only what the message still refers to.**

## 0.4.4 - 2026-09-21

He ran it and wrote down what it drew.

### Added

- **`tofu frame` renders the interface to standard output and exits**, at any width and height, with `--plain` to strip the colour. The interface can now be read without being run, which is how three of the fixes below were found.
- A progress line is not here yet. What is: the running line now reads `· (12) tools · jev 6 · shell (1) · 44s · [#c11]` and never names a tool.

### Changed

- **The clock starts when you press enter and stops when you have the final answer.** It no longer restarts on a phase, a request or a tool call, and only the word beside it changes. `requesting` appears once, before the first response of a message, and never again in that turn.
- **A short id is the last six characters of the real one, not the first.** Every session id begins `turn-` and a hex timestamp, so for months every id on screen was the same six characters: `#turn-1` under everything.
- **The running line counts tools rather than naming them.** A shell keeps its own count, because a process that outlives its call is a different kind of thing.
- Headings render as headings. A second level heading printed its own hash marks, and every markdown fixture was a short handwritten string, so nothing caught it. The fixture is now a real recorded answer of 583 words.
- Brackets mean a thing can be activated, `[1] chat` and `[#c11]`, and the whole label is the target, not the bracket.
- A finished turn reads `cooked for 44s`.
- **The arrow keys belong to the input and never move the transcript.** The wheel, `pgup` and `pgdown` scroll. No key does both.
- The composer's tint covers every row of it, at every colour depth.
- A shell command draws in its own colour rather than the same dim as every other tool call.

### Fixed

- **The vendor's own tool-use id was on screen.** `#toolu_` was Anthropic's identifier for a call, printed directly. The interface mints its own id per call and pairs it with the result.
- The placeholder's first character looked like a leftover letter you could not delete. It was the terminal cursor drawn as a block over it, now a bar.
- The greeting stayed at the top of the transcript for the whole session. It goes at the first message.
- A model named by a subscription is drawn that way everywhere, including the frame header, which still read `openai/gpt-5.6-sol` for a model a Codex subscription serves.
- Three placeholder examples, one picked per session, instead of an instruction that repeated the hint line below it.

## 0.4.3 - 2026-09-21

The interface he designed, and the first tool a judgment clearly wins.

### Added

- Five views, on five digits: chat, work, file edits, sub-agents and shells. **Chat folds every tool call into one running line and never draws a row per call.** The command, the result size and the gate verdict all move to work, which draws each call whole with its arguments and its output. `ctrl+o` jumps to work rather than expanding the transcript.
- **Every event carries an id**, drawn as `#a3f9c1`. Typing an id jumps to that event in work. The record holds a full uuid and the screen prints the first six; an id that matches two events resolves to neither rather than picking one.
- File edits is a diff feed with a sidebar of the agents in the session, active and finished separately. Each change names who made it, where, when and its id. **A path is written as a terminal hyperlink**, so it opens in whatever editor the system already hands it to. Nothing configures an editor.
- Shells: any process an agent started that outlives its call, a dev server, a build, a test run. `tofu shells list`, `tofu shells log <name>` and `tofu shells kill <name>`, and `k` kills from the view. The registry is a real directory, so the command line and the interface see the same processes.
- Settings persist. Global and project, project wins, and the view names the file a value came from. A change is on disk before the next keystroke. **A warning appears only when a setting that truly needs a restart has changed**, from a snapshot taken when the screen opened. Typing filters, and a row whose value differs from its default is marked. `tofu settings get|set` reads the same table, and `tofu reload` re-reads rules.
- **`tofu frame` renders the interface to standard output** at any width and exits, so it can be read without being run. `--plain` strips the colour.
- A models picker grouped by whatever serves each model, and an update check that runs in the background, never blocks, and stays silent when there is no network.
- Input history on up and down, restoring the draft. A pasted image becomes a numbered chip where it was pasted, and the sent message draws a tree naming what went with it.
- Markdown renders while the answer is still streaming. A line already drawn does not change shape when the next delta arrives.
- `tofu shells` and the settings verbs aside, `konst` gained a memory ceiling and a worker count for mutation runs, and a cap on the diff table.

### Changed

- **A model is named by what pays for it.** `claude-sub/claude-opus-5` when a subscription serves it, `anthropic/claude-opus-5` only when a direct API key does. The same model reached two ways bills two ways and the prefix is the only thing that says which. The suffix is derived from the subscription's own name, so a new one needs no extra field.
- **The turn has no decision cap.** It ran to forty gated tool calls and stopped mid-sentence. Any cap is a setting now, and zero means none. `--max-decisions` is refused by name.
- `glob` returns at most 300 paths and says how many matched. Uncapped it returned 6.3 MB for the pattern `*`, and across the recorded corpus that one tool was 94 percent of every byte the model read. **Capping it cuts 93.1 percent of all tool result bytes.**
- The bottom bar is two rows: context as a value over a value with a bar, both quota windows each with its own percentage and reset, and read, written and cached as three separate numbers. **The reset is the last thing dropped as the terminal narrows**, not the first.
- The top row reads path, branch, `source/model`, the session name with its id, and the session clock. No version string, no arrow.
- A blank line separates a person's message from the answer above it, the break between two turns is larger than any break inside one, and the activity block has a gap above it.
- Sub-agents carry a state rather than a tab each: working, waiting for an answer, in review, parked, errored, finished.

### Fixed

- **A mutation run reached 30.6 GB on the owner's machine.** The runner allowed four test binaries at once at thirty times the clean run with no memory bound, and killing it killed only the wrapper. It now runs one at a time inside a job object with a 4 GiB ceiling the kernel enforces, and cancelling kills the whole tree.
- `internal/transform`'s diff built a table the size of one file's lines times the other's, with no bound. A large enough diff reached it without any mutation at all. It is guarded before the table is built, so no mutation to the surrounding loops can defeat the guard.
- The package's own test binary never finished when run outside `go test`, because it hashed the working directory rather than its own. It resolves its own directory now and exits in half a second.
- A killed process was sometimes recorded as having exited on its own, from a race between the kill and the waiter. Both now agree through one lock.
- `grep` and `search` walked the whole tree reloading every ignore rule at every directory. The walk over a 3.3 million line tree fell from parity with a full crawl to about a second.
- Four wall-clock tests asserted the worst frame of a run and failed whenever the machine was busy, at 119 ms against a 16.7 ms budget while the average stayed at 1 ms. They assert the median and print the worst.
- Two published bench reports carried the owner's email, username and machine paths in transcript rows. Scrubbed, and the transcript column that carried them is gone.
- The transcript no longer sinks to the bottom of an empty screen, and the fold line stops alternating between grey and blue on every tool call. It carries the tool count, the jev count and the elapsed time, and no longer a byte total.

## 0.4.2 - 2026-09-21

It stopped making the same mistake twice, and it stops itself when it starts.

### Added

- The model can state a plan, mark one item running and mark it done, and the plan draws in the session view directly above the running row without moving the transcript. One item runs at a time and a second is refused. An item is named by its words, never by an index.
- `tofu sift` reads a shell result against the task that asked for it and marks what is still worth reading. It is in shadow: nothing is removed and the model is given the output whole. Standard error, the chunk carrying the exit status and the first and last chunk of standard output are never candidates.
- A policy file may declare a `schema` other than the gate's, and its thresholds load with it. A threshold that is not a number fails the load with the file and the line rather than reading as zero.
- `tofu check` writes a ledger row carrying the fingerprint of the command it judged, so a judgement made by hand at the command line is found as precedent by the same call inside a run. A replayed row carries the fingerprint of the row it replays.
- A turn stops itself when the model calls the same tool with the same arguments and gets the same result three times inside six calls, and it says which tool, with which arguments, and how many times. Before this it ran to the forty step cap and stopped without explaining anything.
- The prompt carries the working directory, today's date, the platform, whether this is a git repository and the branch, and it reads a `CLAUDE.md` or an `AGENTS.md` walking up from where you typed `tofu`. The nearest file wins. The text is capped and says which file it cut and by how much.

- `fetch` drops a list or table row whose whole content is a link, because that is navigation, and the result says how many units and how many bytes went. A code block and the page title are never dropped. Over three large reference pages this removes 16.2 percent of the text, and 19.9 percent of the Node file-system reference, without losing anything that answered the question.

### Fixed

- `tofu doctor --json` carries a `schema` field for a point that is not the gate, and no longer reports a policy it cannot read as making the binary unusable.
- An image pasted before the first send of a session lands in that session's directory rather than in a different one, and the session body records the file, its size and its format. A paste in a directory with no recorded session works.
- The request that writes a turn's last word now carries the same tool definitions as every other request, so a turn that ends at a cap no longer pays for an uncached prefix. That block is 5,960 tokens, 62 percent of the cached prefix, and it was being bought again on every capped ending.
- A project's own `CLAUDE.md` no longer sits inside the cached part of the prompt, so opening a second project stops invalidating the cache of the first.

## 0.4.1 - 2026-09-21

Typing `tofu` in a project you have never opened walks you through it, the running row says what it is actually doing, and the model can reach the network.

### Added

- **the first run.** With no subscription signed in and no key stored, the app draws what is missing and the command that fixes each, instead of an error. Run `tofu login` in another terminal and it notices within a second and moves on without a restart. The key is never in a frame, a log or a recorded session
- **`fetch` and `web_search`.** A page comes back as units a model can use rather than as markup: 67,692 bytes of one documentation page became 37,274, with every code block intact and links carrying their targets. A page too large becomes a handle. A page that is not text says so. Every fetched page arrives between markers with a per-call random id, as data that cannot issue an instruction. The search provider is catalog data, so changing it is a file rather than a commit, and with no key stored the tool is absent from the list rather than failing when called
- **`tofu why` shows the ten nearest precedents** for a decision, why each one is on the list, and which of them carries a human outcome. A decision now records a fingerprint of the call it judged, so two runs of the same command against different arguments are recognised as the same kind of decision
- confidence is computed here from the vendor's own published formulas rather than read off the response, and both numbers are kept. They agree to within 0.027 across 1,751 recorded answers
- three rules for what a test may not do, in the catalog, so they reach any repository: an assertion that only checks a value exists is not an assertion, a mock stands at a boundary, and a test file covers the empty case and the boundary
- a recorded session carries the ceiling it ran at, the target that came from it, and one sentence saying whether automatic compaction was on and why. `tofu session info --json` reads all three
- `symbols`, which answers which line declares a name and which function each call sits in, from `go/parser` with no cgo. `grep` answers neither
- a bash result carrying a `file:line` citation is checked before the model sees it, and a citation that does not resolve is refused by name rather than warned about. Results this binary generated are not checked, because their citations are true by construction

### Changed

- **the running row has three phases and its clock counts the work.** `requesting` while the provider has said nothing, `thinking`, then `working` with the tool's name. The count starts when the first token arrives, so it no longer includes the wait. A phase holds for 400 ms before another can replace it, which is what stops it flickering between two tool calls, and the whole row is one colour per phase. A turn closes with `finished in 13s`, and the wait only when the wait was longer than the work
- a finished step is drawn where the running row was, because the transcript is bottom aligned now rather than padded downward
- **the ceiling tofu operates under is its own, and it does not move with the model.** A million token window does not mean a million token request is a good idea, it means an expensive one. The model's window is a wall that refuses a request that could not have worked, and it comes from a registry with a shipped offline snapshot rather than being typed into each model's file
- a token estimate now counts the instruction prefix and the tool schemas, which every request pays for and nothing counted. It was up to 84 percent out against what the provider billed and is now 14
- a child that writes outside its paths asks for the path once and waits, instead of the refusal being something a worker can route around
- a tool call repeated inside one turn is answered once. A write to a path clears it, and a cached answer says it was cached
- `read` on a path that does not exist repairs it when exactly one file under the working directory has that name, says it did, and refuses with both named when two do
- a fork carries what was found rather than a list of byte counts. The new session gets one line per source with what came back, instead of a list of calls and their sizes

### Fixed

- **a spawned child's session events were written twice.** The child records itself and the parent wrote the same row again afterwards

## 0.4.0 - 2026-09-20

boji is tofu. The minor number carries it because the name is the command you type and the directory your history lives in, which is exactly what a version promises.

### Changed

- **the binary is `tofu`.** `tofu run`, `tofu session`, `tofu usage`, every verb as it was, with the same flags and the same exit codes. Install it and the old `boji.exe` can go
- **the data directory is `.tofu`**, in the project and at `~/.tofu`. The first run that finds only the old one copies it and says so, with the counts, and the old directory is read and never touched: it is still there and deleting it is yours to do. A second run copies nothing and says nothing. An interrupted copy never becomes the new directory, and the next run throws the partial away and starts over. Your seventy-one recorded sessions and both your logins came across and `diff -r` reports no difference
- the thirty-one `BOJI_*` environment variables are `TOFU_*`. Most gate a test skip, so the skip names were compared one by one before and after, and none appeared or vanished
- the five tools the model can call are `tofu_lint_comments`, `tofu_rules_check`, `tofu_judge`, `tofu_why`, `tofu_replay`. The four recorded sessions that carry the old names still read and still replay

### Not changed

- ticket ids stay `BOJI-NNN`. An id is an identifier and not a brand, and three other tickets reference each one
- `CHANGELOG.md` and the planning documents keep the words they were written with. They are the record of what was decided when it was called that
- `bench/corpus/` keeps one old environment variable name inside a recorded command and one old import path inside a size fixture. Both are recorded evidence with state hashes: changing a byte changes the measurement

## 0.3.7 - 2026-09-20

The last version under the name boji. A model is named by its provider, a role picks the model, and the composer takes what you type while a turn runs.

### Added

- **a model is `provider/name` and nothing else resolves it.** `anthropic/claude-opus-5`, not `claude-opus-5`. The catalog is a directory per provider under `catalog/models/`, with the subscriptions in `catalog/subscriptions/`, and a file missing a required field is refused by its own name and the field it lacks rather than skipped. `boji catalog` counts every kind and prints what each one requires
- **a role binds a model to a job.** `catalog/roles/turn.yaml` and `catalog/roles/child.yaml`, one field, `model`. The turn you drive and the children it spawns can run on different models on different accounts, so a child can work on the Codex subscription while the turn runs on Anthropic. Nothing is bound in the shipped tree, so a plain run resolves exactly as it did before, and `boji models` says which role reaches which model and which role has nothing bound
- **typing while a turn runs queues instead of doing nothing.** Enter puts the text in a queue, clears the composer and shows the row in the transcript marked as waiting. Several queue in order, any of them can be removed before it runs, and the mark clears when the model actually receives it. A message queued mid-turn reaches the model at its next step as a message from you; one queued after the last step starts the next turn. Ctrl+c drops the queue
- **the session record keeps what was read**, every path and its size, findable without reading the whole body, and switchable off. A session can be ended and still resume, and a session past its lifetime is listed as expired rather than deleted. The default lifetime is never
- a created file in the `file edits` tab draws every line it wrote marked as added, and the tally and the content agree: a file reported `+16 -0` draws sixteen added rows. A file too large to draw says how many lines it has instead of drawing nothing. The first row of the sidebar is `feed`

### Changed

- `write` says whether it created the file or replaced it, and a replacement comes back with its diff. A caller can test for a file that changed underneath with `errors.Is` rather than by reading the file again to guess why the write failed
- a child has a state on every exit path, including a cap and a cancel, and a child that returns normally reaches `in_review` rather than `finished`, because finishing is the orchestrator's word. A parked child keeps the work it had already done

## 0.3.6 - 2026-09-20

Orienting in a repository, and the transcript getting its conversation back.

### Added

- `project_report`, one call that answers what a repository is: its size, its languages, its roots, its entry points and its documentation, from one walk that honours `.gitignore`. On this repository it returns in 17 milliseconds. The two `find` commands it replaces, taken from a real recorded run, take 7 to 8 seconds each on a warm cache and one of them spends 7.6 seconds to produce 13 bytes
- a fourth tab, `file edits`, holding every diff a turn made, newest first, with a sidebar of the agents that made them. The transcript keeps one row per edit saying which file changed and by how much. A thirty edit turn costs 69 rows in the conversation where the diffs would have cost 369
- a session has a name you can type. `boji session rename <id> <name>`, and every verb that took an id now takes a name. A session created now is given a readable one
- a bash command has its own deadline, two minutes by default and ten at most, and `timeout_ms` to set it per call. A command that outruns it is stopped and the model is told how long it ran, as a result it can act on rather than a turn-ending error
- every tool says when its answer was degraded, by name: a walk that hit a cap, a file that would not parse, a binary skipped, a cached result gone stale, a command stopped. A degraded result is never rendered as a complete one

### Changed

- the prompt names `find` and says why it is slow here rather than vaguely preferring tools, with the real numbers from the run that prompted it
- a recorded step carries the four band caps it was measured against, so a session read after the caps move is not shown against numbers it never used

## 0.3.5 - 2026-09-20

`boji changelog`, and a running child that says what it costs.

### Added

- `boji changelog` prints what changed since the version you last read, from a copy carried inside the binary, so it works in any directory. Running it again says there is nothing new. `--all` prints every version and `--json` carries every version as data. Neither of those two records that you read anything
- a running child in the activity block carries its elapsed time and the tokens it has spent, live, where both read as nothing until the child returned
- the tools that walk files read `.gitignore` and honour it. In this repository `grep TODO|FIXME|XXX|HACK .` went from 82,669 files and 2.57 GB to 636 files and 3.4 MB, and its result from 15.6 million tokens to 117, because the walk used to read nine cloned repositories on every search. `glob`, `grep` and `search` each take `include_ignored` to turn it off, and say so in the result when it is off. A `CLAUDE.md` or `AGENTS.md` is read whatever an ignore file says, so an agent can always find the project's own rules
- **you can select text in the app while a turn is running.** The app repainted four times a second to move the spinner, and a repaint clears whatever the terminal has selected, so copying out of a running turn was impossible. It now paints only while something is actually moving: a running tool call still animates, a turn waiting on the model does not
- `ctrl+y` copies the last answer, `alt+y` copies the last tool call with its result, and `/copy` does what `ctrl+y` does. A copy that fails says so rather than reporting a success it did not get
- a slash at the start of the composer opens a command menu, which filters as you type. Four commands: `/session`, `/crew`, `/settings` and `/quit`. A slash anywhere else is ordinary text, so `explain /internal/turn`, `// a comment` and `(/ ` all reach the model unchanged, and `\/` escapes one. Previously `/settings` was sent to the model as a task

- **a stopped turn keeps the conversation.** Pressing ctrl+c and then typing "continue" used to reach a model that had never seen anything you said, so it started the task over. Every message a turn produced is now carried whatever ended it: a cancel, a cap, a model error or an answer
- the turn is written to disk while it runs rather than after it ends, so a ctrl+c, a crash or a model that never answers no longer loses the tool calls already made. Measured at 3.8 to 4.9 ms on a turn with twenty tool calls, against real recorded turns of 100 to 233 seconds
- a reply cut off by the provider's output token limit is recorded as `truncated` rather than as a finished answer, and any tool call it was part way through issuing is refused rather than run, because a lenient parser can salvage arguments that look complete and are not. The model is told why and can issue the call again
- `end_reason` in a stored bench row can be `output_truncated`, for the same case

### Changed

- an outcome name is written in one place rather than two, so the name a session records and the name a session reads can no longer drift apart
- the outcome a retired wall clock cap produced is named as retired. A session recorded before the cap was removed still reads
- ctrl+c stops the turn once and says so. It used to cancel silently, so pressing it again wrote the same line again and again with no sign anything had happened. A second press while the turn is unwinding does not quit, and the footer says so
- a turn you stopped reports as stopped. It used to report the provider error the cancel produced, in red, including the vendor URL

## 0.3.4 - 2026-09-20

The gate can refuse, and the instructions are cached from the first request.

### Added

- `boji run --gate off|shadow|enforce` chooses how the tool gate behaves for one run, and `--no-gate` is the same arm as `off`. With no flag the policy's own declared mode decides
- under `enforce` a `deny` refuses the call: the tool does not run, the model is told in the result it reads and why, and the turn continues so it can take another path. A gate that cannot answer refuses as well, because a check that cannot run costs whatever the tool was about to do
- under `enforce` an `ask` with nobody to answer refuses and says nobody was available, which is every `boji run`

### Changed

- `boji usage` draws a bar per quota window, right-aligns the percent, and writes a reset a person can plan around. `resets in 75h26m` is now `resets in 3d 3h`, and under an hour it is minutes. A window the provider did not report is hidden rather than printed as unknown, and only the fullest window is coloured, at a threshold of 50 and 80 percent
- the app's status bar and `boji usage` draw the same quota through the same code, so a window at 72 percent looks identical in both. That cost the status bar its clock: `62% resets 18:00 (in 6h)` now reads `62% resets in 6h 25m`
- a percentage and its bar can no longer disagree. The headline rounded half to even and the meter rounded half up, so the same window could print two different whole numbers
- the first request to Anthropic now caches the whole instruction prefix rather than none of it. On this repository's own prompt the first send writes 5,931 tokens and the second reads all 5,931 back. Before, the first send cached nothing at all: the marker sat on a block too short to meet the vendor's minimum cacheable length, so a session that never got a second turn paid full price for every token of it
- the cost of that is one cache marker. The wire allows four per request and the instruction prefix now takes three, so on a long conversation the older history anchor is dropped and a read after the tail changes falls back to the system prefix alone

### Nothing turned on

Every policy still declares `mode: shadow`, so a deny still records and still runs. `enforce` exists and nothing selects it. The app cannot ask a person yet either: the verdict and its answers are drawn, and no key is bound to them.

## 0.3.3 - 2026-09-20

- the model is told where it is: the directory, the date, the platform and the branch, plus `CLAUDE.md` and `AGENTS.md` walked up from the working directory, nearest winning. It used to run a tool to discover rules written where it was standing
- a tool result is cut at 32 KB rather than 8 KB. Measured against this repository, 400 of its 401 Go files now pass whole, where before a read was a read plus a fetch and each fetch threw the cache away
- a turn has no wall clock limit. It stops when the work stops
- the activity block sits above the composer and carries the running turn and every running child

## 0.3.2 - 2026-09-20

- a second message in a session continues the conversation. Every send used to start again from the task alone, so nothing said in the turn survived to the next one
- one unreadable session record no longer takes down `boji context`. The row is skipped with its reason and the rest list

**The patch number moves on every accepted batch, not only on a big one**, by the owner's instruction on 2026-09-20: "please anything you adding to today that is not upadting to major, please updated the 0.3.x because i feel i'm stuck and you did a lot of changes and yet, i'm not on pair of what is there." So 0.3.1, 0.3.2, 0.3.3, and the minor still carries a break. The bump and the rebuild happen in the same turn, so `boji version` is how he tells what he has.

## 0.3.1 - 2026-09-20

Readable output, a screenshot you can paste, and a frame that holds at any alphabet.

### Added

- a screenshot pastes into the composer with `ctrl+v` or `alt+v`, from a picture on the clipboard or from a copied file, saved beside the session record. Snipping Tool and browsers put a PNG on the clipboard directly, so the common case needs no conversion. A Print Screen bitmap carries a zero alpha channel and is forced opaque, without which the image would be written invisible
- a tool result says whether it failed and how big it was, so a failed call is visible rather than inferred from its text, and the run's one line summary carries a size
- `boji doctor`, `boji usage` and `boji models` each take `--json`, carrying every field the readable form collapsed

### Changed

- `boji doctor` answers on its first line, `ready` or what is missing with the command that fixes it, and is fifteen lines where it was twenty. Six policies with identical thresholds are one line that says six. The repository root is printed once. `boji usage` and `boji models` follow the same shape, and twelve excluded models collapse to five counted lines carrying the catalog's own reasons
- `boji` no longer asks which subscription runs the session. It starts on one and the header names it
- the layout measures terminal cells rather than runes, so a path or a branch name carrying CJK or an emoji no longer overflows the frame. Measured at 113 columns inside an 80 column frame before the fix
- a rate limited quota check says so, where it used to report the quota as unreadable

## 0.3.0 - 2026-09-20

The app, and the gate. Typing `boji` opens something you work in, and the turn still asks a typed question before it acts.

### Added

- `boji` with no arguments is an app. It comes up in the directory you are standing in, takes a task in plain words, works it in a loop and shows every tool call and every result as it happens. Ctrl+C stops the turn and leaves the app up. With a credential missing it names what is missing and offers to run the login rather than printing an error. Every verb behaves exactly as it did, so a script that drives Boji is unaffected
- `boji` starts on the first subscription signed in and asks nothing. The top bar carries the wire and the model it resolved to, `anthropic → claude-opus-5`, so what it chose is visible without anybody being asked, and the quota line follows the subscription in use. An earlier build asked which subscription to use before the session started; that question is gone, and `/model` is where it changes
- the app has a tab strip, `tab` and `shift+tab` to cycle, a digit to jump, `esc` back to the session, and a click on a name. In the session view a digit types a digit, because that view is the composer
- a settings view, listing every provider with the file that decided it: the credential store, `~/.boji/.env`, or the last decision. A stored key is shown as four characters and never in full
- `boji doctor` says which arm spends money and which spends a subscription quota, one line per wire, read from the same place a run reads it so the two cannot disagree
- **breaking:** `boji rules check` no longer prints an override rate and prints `fires: N, blocked: N` instead. The rate could only ever be zero, because nothing in a rule scan asks a person anything, so nothing could record an override. The `--json` report and the `.rules.jsonl` record lose their `overridden` field with it. It comes back when there is a place a person waves a fire through, which is the gate's ask verdict rather than a batch scan
- `boji sift` reads a message on standard input and prints it with what is worth reading kept and the rest elided, and `boji sift --restore` puts it back byte for byte. `--arm` picks how it decides: `signpost`, the default, drops a markdown heading or a short line ending in a colon, free and instant; `brevity` is the word count and banned word list; `jev` asks the typed decision. On a hand labelled set of 57 paragraphs the default agrees 89 percent of the time, Jev 84 to 86 at a third of a cent and a second per message, and the brevity arm 26
- `boji login openrouter` asks for the key without echoing it, proves it reaches Jev with one call, and only then writes it to `~/.boji/.env` at mode 600 where the platform honours it. A key that does not answer leaves nothing behind. The key is never an argument and never reaches a log. A project `.env` still wins over the stored one
- `boji run` has six tools instead of three: `glob`, `grep` and `edit` join `read`, `write` and `bash`. `edit` takes an exact span of text and replaces it
- `boji run` can call Boji. `boji lint comments`, `boji rules check` and `boji judge` are tools a turn may use, so an agent runs this project's own checks instead of imitating them. Recursion is bounded at depth 2
- a child that reports done gets asked whether it is. `boji run --done-review typed` puts the claim through the `stop_check@1` battery and re-opens the child once, in words, with the reason and the decision id, so `boji why <id>` prints the chain behind the re-open. `--done-review cheap` is the arm that decides on whether any tool call ran clean, and `off` is the default and the behaviour until now. A Jev error leaves the claim standing with the reason on the child's row rather than re-opening on a broken backend
- a spawn onto a path another child already holds returns that child, the glob that collided and what it has reported, instead of an error. The roster still refuses; the refusal now carries the one correct move
- a turn can hand work to a child that owns its own paths, refused at the write if it strays, and refused outright if two children overlap. The child's row reaches the ledger as a turn of its own and the parent's cost covers the tree it started. `--no-crew` is the off arm
- a context budget, with the byte-per-token figure measured on this project's own content rather than assumed
- a session that outgrows its budget forks instead of being rewritten in place. The old session ends whole and a new one begins carrying the work forward, so the next request is one fresh prefix and then appends. Rewriting the middle of a live conversation costs 58 percent more than doing nothing at all, because it destroys the cache it was meant to save. The fork carries handles to what it dropped, which is instant; a written summary was measured at 21,931 ms, stale before it arrives, and saves 5 percent. The off arm is in the loop configuration and has no flag yet, unlike `--no-gate` and `--no-crew`
- `boji run` asks the tool gate battery before every tool call and records the answer. The decision row names the turn and the turn row names the decision, so either one leads to the other
- `boji run --no-gate` is the arm that turns the gate off, and `--max-decisions` caps the spend. A turn that reaches the cap ends with its own outcome rather than continuing quietly
- `boji rules list` and `boji rules check [path]` run the rules in the catalog over a tree and print every fire with its mode, whether it blocked, and the override rate. `--catalog <dir>` points at a scratch catalog, so an enforced mode can be tried without touching the one that ships
- a rule may declare an exception in its own file. `em_dash` declares `except: quoted`, so an em dash inside a fence, an indented block, a backtick span or a double-quoted span is the author quoting rather than the author writing
- a turn session file carries a schema version, and a decision row carries the turn it came from

### Changed

- the shell tool runs `sh -c`, not `cmd /C`. On Windows `cmd` has no way to escape a quote inside a quoted argument, so 24 of 32 recorded quoted commands reached the tool as a literal backslash-quote, matched nothing, and returned empty with exit 1, which a model reads as an answer
- the request to a subscription marks the conversation as cacheable, not only the system prompt and the tool list, and the cache lives an hour instead of five minutes. The token count is unchanged; what those tokens cost is not
- the Codex request carries a cache key for the whole session, so its own prefix cache can find the previous step
- `boji doctor` finds a key stored in the home file, from any directory, and still names where it looked when there is none
- `boji run` sets a system prompt naming the tools it has. A tool a model is not told about is not offered, which is why the first run with six tools called neither `glob` nor `grep`
- `boji rules list` and `boji rules check` carry their catalog in the binary, so they work in any tree rather than only in this repository. A project with its own `catalog/rules` still wins, and both verbs name which set decided, as a first line in text and as an `origin` field in `--json`
- **breaking:** `boji rules list --json` emits `{"origin": ..., "rules": [...]}` where it emitted a bare array. A reader that expects an array at the top level needs one field of indirection
- `boji_judge` takes a `state` and a `battery` instead of a raw json body. A model could not guess the old shape: in one recorded run it tried six times, first with a list, then with four different spellings of a question type, and failed every time
- the stop check corpus is nineteen frozen files under `bench/stopcheck/`, not whatever the live ledger happens to hold. Running a turn in this repository no longer changes what a measurement is measured against
- the three copies of the percentile arithmetic under `bench/` are one package, `bench/stat`. The three were identical, so no published figure moved
- the boji arm of `bench harness` no longer measures whichever binary invoked it. It builds `./cmd/boji` and measures that, or measures the one named by `--boji <path>`

### Removed

- `boji bench` is gone, with its five targets. The measurement lives in its own binary, built from `bench/cmd`, so an edit under `bench/` cannot break `boji login`, `boji why` or any other verb. Run `go run ./bench/cmd <target>` from the repository root, or build it once with `go build -o bench ./bench/cmd`. `boji bench api` becomes `bench api`, and `cost`, `wording`, `turn` and `harness` follow the same way. Every flag is unchanged and the reports are byte for byte what they were

### Fixed

- the gate was off in every directory except this repository. The policy was looked for beside the project rather than inside the binary, and the key was read from `./.env` alone. The policy ships in the binary now, a project may override it, and the row says which one decided
- the Codex wire made up a new session identity for every request, so nothing it sent could match anything it had sent before
- a turn session file never recorded its schema version, so every one written before today reads as schema 0 against version 1
- `boji judge` never recorded which state builder made its state, so `boji why` said the writer had not adopted it. Every row from `boji check` had it and no row from `boji judge` did

## 0.2.0 - 2026-09-18

The record. Every decision can be explained and re-scored without asking the model again.

### Added

- `boji why <id>` prints the chain behind a decision: every answer with its distribution, the verdict, the rule that fired, the threshold it compared against, and a line naming any question that sat inside the dead band
- `boji replay --point <name> --set <threshold>=<value>` re-scores every recorded decision against changed thresholds, with no network call in its import graph, and counts how many changed verdicts now disagree with a recorded outcome
- `boji check '<command>'` decides on a real shell command and records the row without blocking anything, and `boji label <id> <outcome>` attaches the answer a person would have given
- `boji lint comments` reports every comment in the tree with its position, using the standard library parser rather than a text match
- a policy: thresholds as catalog data, a closed verdict type, authority nouls that relax and never tighten, and a dead band taken from the measured rerun spread
- a state builder with a version derived from its own shape, recorded on the row

### Changed

- a ledger answer keeps the kind it was: a noul is a number, not a probability parsed out of a field named for a different answer kind. Schema 2, and rows written under schema 1 still read
- the request cap is two documented token limits instead of one invented byte number, with an estimator built from five measured points and deliberately pessimistic where it has no data
- a bare catalog name is refused when more than one version exists, naming every version it found, rather than silently taking the newest

### Fixed

- `boji check` put the command itself into the recent user messages, so a question about what the user asked for answered 0.91 about a request nobody made, relaxing a force-push from ask to allow. It is 0.03 and ask now
- `boji doctor` reported the key as missing while `boji judge` was using it, and now names where the key came from
- a converter existed three times and two copies silently dropped a choice option's criteria

## 0.1.0 - 2026-09-18

The instrument. A typed decision can be asked, validated, recorded and measured.

### Added

- `boji judge` reads a state and a question battery on standard input and prints typed answers, writing a row for every call and answering a repeat from cache with no network
- `boji bench api` measures latency by state size and question count, rerun agreement, the option ceiling and cost per decision, and writes a dated report
- `boji bench cost` compares Jev, two frontier models and a plain regular expression on the same six decisions
- `boji doctor`, `boji catalog resolve`, `boji version`
- the Jev wire with structural validation: probabilities that sum, a chosen option inside its criteria, an argmax that agrees, a score level count that matches
- the decision ledger, append only, one file per day, with the full distribution on every answer, and a replay cache keyed on canonical JSON
- the question catalog with a build-time linter, and a YAML subset parser written by hand rather than taking a dependency
