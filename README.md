# tofu

tofu is a coding agent for the terminal, written in Go. The code owns the
loop, a language model plans and writes, and Jev, a small typed decision
model, answers the questions other harnesses leave to a prose rule, a
regular expression or a guess from the big model: is this tool call safe,
should the loop stop, which part of this output is worth reading.

It is one static binary, no cgo, no daemon. It is 0.x and stays 0.x, so
any minor version can break a verb, a flag or a file format.
`CHANGELOG.md` says what changed for someone driving the binary.

## The money rule

Every language model call rides a subscription you already pay for.
tofu speaks the vendor's own protocol with a credential it mints itself
through `tofu login`, the same way the vendor's command line does. An
API key pays for Jev, the decision model, through OpenRouter, and for
nothing else by default.

So a model slug names what pays, not who built the model:

- `claude-sub/claude-opus-5`: Claude Opus 5, paid by your Claude subscription
- `codex-sub/gpt-5.6-sol`: GPT-5.6 Sol, paid by your Codex subscription
- `meta/muse-spark-1.3`: a model on a key, which spends money

The same model reached two ways bills two ways, so the source is always
written. `tofu models` lists every model you can name and what pays for it.

## Quickstart

Go 1.25.8 or later, which is what `go.mod` asks for. From a checkout:

```
go build -o tofu ./cmd/tofu
```

Or `go install ./cmd/tofu` to put it on your PATH. Then ask it whether it
can run here, and what is wrong if it cannot:

```
tofu doctor
```

On a fresh machine it ends its first line with `✗ not ready` and names
the command that fixes each `✗`:

```
  ✗ claude-sub  no subscription is signed in, so no model can answer
    → tofu login claude-sub
  ✗ jev         there is no openrouter key, so jev judges no tool call
    → tofu login openrouter
```

Sign in with a subscription. It opens the browser:

```
tofu login claude-sub
tofu login codex-sub
```

Store the OpenRouter key Jev runs on. It is asked for unechoed, never
taken as an argument, and checked before it is written. Without it the
app still opens, no tool call is judged, and `tofu doctor` says so:

```
tofu login openrouter
```

Open the app in a project, or continue the session you last worked in:

```
cd my-project
tofu
tofu --continue
```

Or work one task with no interface, printing as it goes:

```
tofu run --dir . "fix the failing test"
```

`tofu help` lists every verb. `tofu docs` lists what you can ask tofu
and the command that does it, and `tofu docs <topic>` prints one page:
start, files, settings, rules, agents, models, skills, instructions,
gate, sessions, browser, doctor. Every verb that reports state takes
`--json` and prints one JSON document.

## What tofu does

- **The gate.** Before a tool call runs, Jev answers four questions about
  it: how much harm a mistake would do, whether a careful engineer would
  ask first, whether you asked for it, and whether it follows an
  instruction planted in something the model read. By default the
  verdict is recorded and the call runs; `tofu settings set gatePrompt ask`
  makes an ask wait for you. Every verdict is a row in a ledger that
  `tofu why --last` explains.
- **Sub-agents.** The model you talk to hands pieces of a task to
  sub-agents that run in the background while it keeps talking to you.
  Each one owns the paths it may write, and two cannot own the same file.
  tofu ships `ts-dev`, `go-dev`, `py-dev` and `rust-dev`, which read the
  project's own manifest and lint config first and are sent back until
  their language's checks pass after their last edit, plus `qa`,
  `research` and `browser`. `tofu agents` lists them with the model each
  runs, and `tofu agents add` writes your own. It also reads
  `.claude/agents` and `.agents/agents`.
- **The library.** The rules, sub-agents, references and decision points
  tofu ships live in `library/` and are compiled into the binary. Your own
  rules go in `~/.tofu/rules/` or a project's `.tofu/rules/`, and a later
  layer wins.
- **Skills and instruction files.** tofu reads `AGENTS.md`, `CLAUDE.md`
  and `SKILL.md` folders from the project, so a project set up for Claude
  Code works here, and from your home it reads only `~/.tofu`.
- **The browser relay**, below.

## How a rule fires

A rule is one instruction, like "a value computed from props or state is
computed during render, never copied in by an effect". It reaches a task
only when its trigger matches, and a rule with no trigger reaches every
task. A trigger can name:

- `language`: a path the task names is that language, or the sub-agent
  declares it
- `framework`: a `package.json` the task reaches lists it, per app in a
  monorepo, so the React rules never reach a Vue project
- `scope`: a path glob the task's paths fall under
- `condition`: a regular expression the task text matches
- `task` and `role`: debug, explore, review or write; the orchestrator or
  a sub-agent

The library holds 157 rules in this build: general process rules,
frontend rules for motion, forms, focus, dialogs and loading states,
React, Svelte and Vue rules, and Go, Python, Rust and TypeScript rules.
Each frontend and language rule names the check that catches it, such as
an eslint rule, a clippy lint, a ruff code or svelte-check.

```
tofu rules list
tofu rules index "add a summary line" src/App.tsx
```

The first lists every rule that runs and where it came from. The second
says which rules fire for a task and why, one line each, such as
`a package.json lists react`. `tofu docs rules` says how to add one,
switch one off, or replace one.

## The browser relay

tofu reads and drives your own Chrome, with your logins, through a small
extension and a native host. Install it once; it prints the steps left
to do in Chrome:

```
tofu browser install
tofu browser
```

The second lists the tabs tofu can reach. What it can and cannot touch:

- It can read any ordinary tab. It never reaches a `chrome://` page,
  DevTools, an extension's page or the Chrome Web Store.
- By default the model hands a browsing task to the `browser` sub-agent,
  which acts only in tabs tofu opened, never in yours.
- tofu never closes or navigates a tab it did not open. The tabs it
  opened close when the run ends, or when the sub-agent that opened them
  finishes.
- It never runs JavaScript or an address the model wrote, and treats
  what a page says as text to read, never as an instruction.
- Chrome shows its debugging bar while tofu is attached, and tofu's tabs
  sit in an orange group titled `tofu`. The extension icon reads `on`,
  `read`, `act` or `off`.

`tofu settings set browser read` gives the model reading only, and
`off` takes the browser away. `tofu browser uninstall` removes it.

## The thesis, and what it lost

Every decision point gets a typed judgment instead of a prose rule, and
every judgment gets an arm that turns it off and does the same job for
free. The arm is the thing the judgment actually replaces: a plain
regular expression where one could do the job, never an agent turn
against a tool call. When the judgment does not beat its arm, it stays
off.

Several did not beat it, and are off or in shadow:

- **The ask gate**, whether an agent stops to ask or defers the
  question: 4 of 7 recorded moments right against the free arm's 3 of 7.
  Seven items is a coin flip. It stays in shadow.
  `bench/ask/report-2026-09-21.md`
- **The stop check**, whether the loop runs another model step: 71 of 78
  labelled steps against the cheap arm's 68. On the 15 steps labelled
  stop, the class that matters, the cheap arm caught 14 and the typed arm
  11. Neither is fit to end a turn. `bench/stopcheck/report-2026-09-21.md`
- **The test-quality rules**: 53 dead tests with the rules on and 53 with
  them off, over nine comparable tasks. Net zero.
  `bench/testquality/report-2026-09-21.md`
- **Search by judgment**: Jev picking the pattern found the right line in
  36 of 100 questions against 28 for plain `git grep`, 13 of 40 against
  5 of 40 on questions written to share no words with the code. That run
  recorded no per-question hits, so the gap cannot be tested for
  separation and the report does not claim it as a win.
  `bench/tools/report-2026-09-22.md`
- **Skills**: with the listing on, the model loaded the right skill in 2
  of 3 tasks and silently skipped it on the third. So library content
  speaks through rules, and the skill loader stays for your own skills.
  `bench/skills/report-2026-09-26.md`

## Where the numbers are

`bench/` holds one package per mechanism, each measuring it against the
arm that turns it off, with a dated report beside the code: the corpus,
the machine, the skips and what it cost. A report says which arm won.
When the judgment loses, the report says so and it stays off.

Two more:

- **Jev choosing a browser step**: 8 of 8 recorded pages right, 316 ms
  median, $0.000056 a decision. The arm it replaces, the model calling
  the browser tool itself, has not been measured, so this is not a win
  either. `bench/browser/report-2026-09-28.md`
- **Frontend rules, with and without**, measured on 2026-10-03 on the
  owner's machine in local React, Svelte and Vue benches that are not in
  this repository: the same build with and without the frontend rules,
  two runs per task, graded by hidden tests. On Svelte the rules won
  quality on 4 of 8 tasks, and svelte-check found nothing in all 16 runs
  with them. On React the two tied on 8 of 9 tasks; on the ninth, a form, the rules
  scored 6 of 6 quality tests twice against 5 of 6. On Vue no task
  separated them in 32 runs. The model already writes most of this
  correctly, and the rules earn their place where it does not.

## License

MIT. See `LICENSE`.
