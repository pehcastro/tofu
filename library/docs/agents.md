---
topic: agents
title: Sub-agents
summary: add a sub-agent for every project or for one, choose the model it runs, and remove it
verbs: agents
---

## What it is

A sub-agent is a helper the model can hand one piece of a task to, like a planner or a reviewer. Each one has a name, a description the model reads to decide when to use it, the model it runs on, and the tools it may use. tofu ships a few, and you can add your own. `rust-dev` also builds gpui-ce interfaces, with the `gpui_` rules when a Cargo.toml depends on gpui-ce. A language sub-agent, `go-dev`, `py-dev`, `rust-dev` or `ts-dev`, is sent back until its gate checks ran after its last edit and passed; a crate with no `#[test]` and no `tests` folder is exempt from `cargo test`, and the gate says so, naming the crate's `Cargo.toml`.

The shipped `advisor` reads each plan the lead sets, in shadow, while the `planAdvisor` setting is `shadow`, its default. It gets your own messages and the plan, never the lead's reasoning, and answers with a `Missing` line for each thing you asked that no plan item does and an `Extra` line for each plan item nothing you said asks for. The lead never sees it, and its next request is the same byte for byte; the answer is in `tofu session trace <session> --kind advisor`, and an advisor that fails writes one row saying why. It runs on the model its row in Settings > Models & roles names, the orchestrator's unless assigned another, at one request a plan set, counted in the session's cost like any sub-agent's. `tofu settings set planAdvisor off` stops it.

When the model hands one a piece of work, it names the paths that sub-agent may change. A sub-agent given no paths still starts: it reads, runs commands and reports, and every write or edit it tries is refused. Sub-agents spawned together on a model whose catalog entry says `prompt_cache: prefix`, or says nothing, wait for the first one's answer so they read the prompt it cached; on a model that caches no prefix they all start at once, and the trace says which led, which waited and why.

In a session the model steers its sub-agents with two tools. `subagents` lists them with the paths each holds, and with a name it reads one's report and conversation or diagnoses what it is doing now. `message` sends one more work, and its `do` stops, kills, releases or backs one up, or with `owns` grants, revokes or replaces the paths it holds. None of these asks the gate. Each sub-agent also has a scratch folder outside the project, under `~/.tofu/projects/<project>/scratchpad/`, where it writes and deletes with no ask. See [Scratchpad](scratchpad). A sub-agent from before a restart holds no paths until a message resumes it, so a new one can start on them. One still working when tofu closed comes back errored, the model is told at which step it stopped, and a report it finished before the close still reaches the model. A sub-agent's one-hour cap counts only its own work, not time spent waiting on an answer. A crash inside one ends only that sub-agent, as errored with the stack in its trace, and a crash in a turn's background task ends only that task or turn. Sub-agents settled for over two hours fold into one row in the sub-agents tab and the chat; a click, `enter`, `f` or a search opens them; a search or a call's reference opens only the agent it found. With `projectManagement` set to `boardy`, a spawn names a `ticket`: its owns become the paths, and the sub-agent moves it to review itself. See [Boardy](boardy). On a ticket the sub-agent proves each acceptance line with an evidence entry naming that line, says where it ended in one or two sentences, and moves the ticket to review leaving the lines that are not its to prove; tofu writes one reported entry from those, with a verdict (done, done with concerns, blocked, needs you or not possible) and each line as met, unmet or not mine to prove. A move to review with an unmet line is refused, naming it. A sub-agent that finds its ticket cannot be done as asked calls `ticket_not_possible` with the reason and two to four options instead of moving it to review: the ticket moves to needs-decision with that question, the report's completion is `not_possible`, tofu puts the question to the person in the chat in the same turn, the done review does not send that run back, and only the person moves the ticket on, by answering there or in catch up. Without a ticket, its last message is the report. Before the lead takes a hand-back, the `doneReview` setting reads it: `screen`, the default, sends the sub-agent back while an acceptance line in its brief has no evidence entry naming it, up to three rounds, and the lead's report says `sent back` with each line. `off` takes the hand-back as it is. See [Settings](settings). `stop_check` reads every hand-back in shadow, and a lead that stops with board work open is asked it about its own stop; the trace says what it read. Clean reports finishing close together reach the model as one turn: each waits 1.5 seconds for the next, 10 at most, and goes at once when no sub-agent still runs. An ask, a report that is blocked, has concerns or needs context, and a report from a run the model resumed never wait. A check never starts a turn. A message you send mid-turn is answered first, before any other call, and Jev decides whether the lead's reply answered it: on 30 written cases it kept every unanswered reply open, where a phrase list kept 5 of 18; while only sub-agents run, the lead is idle and free to talk. Each report ends with a board line, the running count, tickets in review and free work; a lead that ends with free work and empty slots is checked in on while tofu is idle, and any message drops that check-in. The chat keeps only the conversation: the lead's own calls sit under the orchestrator in the sub-agents tab, and a report reads as a sentence, `finished` a ticket `and waits for your review`. A spawn asking for an effort the model lacks runs at the nearest one it has. A spawn on a ticket records its effort in the ticket's thread, and a second raise under one acceptance is refused naming the efforts tried, so the lead rewrites the outcome with `tofu boardy set <ID> --acceptance` instead; a spawn with no ticket is not counted. A sub-agent allocates about 35% less than in 0.5.14: under one vCPU and 128 MB, tofu runs ten at about 0.7 MB each. The lead may change up to 200 source lines a turn itself, in any folder, before it must spawn: a write counts only the lines it changes, and a file read with `cat` counts as read. On the rivals long edit the lead takes 78 s where it took 141 s. In a sub-agent's shell the read-only tofu verbs run: `tofu session find`, `trace` and `info`, `tofu boardy show`, `list` and `triage list`, `tofu models`, `tofu version` and `tofu serve --schema`; of `.tofu` it reads only `.tofu/rules`; `cargo build`, `cargo run` and a binary they built run for a crate it holds a path in, into the project's one shared target, and `--target-dir` is refused; so do `powershell -File` on a script inside a path it holds and `timeout <n>` around any command it may run. A command the gate would ask you about is refused in a sub-agent rather than asked.

A name is one plain lower-case word: letters, digits and `-`, starting with a letter, like `planner` or `ts-dev`.

## A sub-agent with no agent

A spawn that names no agent runs on the lead's model with the tools of the `unnamed` file tofu ships: read, write, edit, bash, glob, search, test, typecheck, format, eval, plan, fetch, whatif, note, digest, checkpoint, forget, project_report, changes, trail, budget, skill and spawn, the tools such sub-agents called in real sessions. Beside them it always gets ask, artifact_fetch, history, reference, the scratch tools, shell with bash, message and subagents with spawn, and the ticket tools when the spawn names a ticket. Its first request carries 34 tools where it carried every tool the lead had, 65 on a fresh project. An `unnamed.md` in `.tofu/agents/` or `~/.tofu/agents/` replaces the list, and `tofu agents` shows which one is read. The lead is never offered `unnamed` by name.

A list naming a tool tofu does not have is refused when it is read: `tofu agents` marks it `✗ refused` with `names the tool "<name>", which tofu does not have`, and every spawn with no agent is refused with the same reason until the file is fixed.

## The checks it ran

When any check a sub-agent ran did not pass, its report lists each check that failed or was skipped, in order, each with its exit code and the first error line it printed; the checks that passed are a count:

    counts: 1 findings noted, 2 checks failed, 41 checks passed
    checks it ran that did not pass, each with its exit code and first error line:
      go test ./... exited 1: --- FAIL: TestAdd (0.00s)
      cargo test 2>&1 | tail -5 exited 0 at the last stage of its pipe, and printed a failure: test result: FAILED. 0 passed; 1 failed
    noted: sent back once: go test exited 1: --- FAIL: TestAdd (0.00s)

A check is a shell command, `test` or `typecheck`. A report where every check passed has no list, and a send-back quotes the same line.

## What the lead reads

The lead reads a sub-agent's report as a card of at most 8,000 bytes. Each part has its own cap: findings 1,600 (600 each), checks 1,200 (240 each), files written 400, warnings 300, the board lines 500, and the sub-agent's last message what is left, up to 4,000 of its own bytes. A part over its cap keeps its head and tail around `...(N bytes cut)...`, and a list over its cap ends `+K more ... in the full report`. The counts line is taken before any cut, so a dropped finding or check is still counted.

When anything was cut, the whole report is kept as one artifact, every check, finding and line whole, and the card names it right under its counts, before anything that can be cut:

    full report: artifact 0eb58161518280f6128acef55c8badd7 holds all 100432 bytes, every check, finding and line whole: read any range with artifact_fetch

When it cannot be written, that line says the full report was not kept and why, and the card still holds its caps. A report of 13,462 bytes reaches the lead as about 6,200, and one of 119,364, whose finding carried a check script's whole output, as about 7,300.

A pipe exits with its last stage, so `cargo test | tail -5` exits 0 when the test failed, and the line names that exit. For `go test`, `go vet` and
`cargo test`, tofu also reads what the command printed: when it shows a
failure the line says `exited 0 at the last stage of its pipe, and printed a failure`, and the check counts as failed.

## Where it lives

A sub-agent is one file, `<name>.md`, read from these places, the first one found winning:

- `.tofu/agents/` in a project, then `.agents/agents/` and
  `.claude/agents/`, in the order the `agentSources` setting names
- `~/.tofu/agents/`: your own sub-agents, in every project
- the sub-agents tofu ships

The model a sub-agent runs can also be set apart from its file, in
`agent-models.yaml`, one `name: source/model` line each. A line in the
project's `.tofu/agent-models.yaml` beats one in `~/.tofu/agent-models.yaml`, and either one beats the model written in the file.

## Change it

Add a sub-agent to this project, run from inside the project:

    tofu agents add planner --description "plans the work before anyone writes" --model claude-sub/claude-opus-5

Add one to every project with `--global`, and give it only some tools with `--tools`:

    tofu agents add --global reviewer --description "reviews a change" --model codex-sub/gpt-5.6-sol --tools read,search,glob

From anywhere else, name the project with `--dir`:

    tofu agents add --dir C:\code\shop planner --description "plans the work" --model claude-sub/claude-opus-5

Run a sub-agent on another model, including one tofu ships:

    tofu agents set planner codex-sub/gpt-5.6-sol
    tofu agents set --global qa claude-sub/claude-sonnet-5

`set` writes `agent-models.yaml` in the project, or in `~/.tofu` with
`--global`. `inherit` instead of a model makes it run on the same model
as the task that spawned it. `tofu models` lists every model you can name.

A model tofu does not know, a tool it does not have, a name that is not one plain word, and a name that already has a file in the same place are all refused, and nothing is written.

Every write prints what changed, `+` added, `~` changed or `-` removed, the file, and the command that undoes it:

    + sub-agent planner  ~/code/shop/.tofu/agents/planner.md
      → undo: tofu agents remove planner

A refusal prints one `✗` line on stderr and the command to run instead, and exits 1. With `--json`, a write prints one document instead.

## Check it

    tofu agents

opens with `Sub-agents · <n> defined`, then a row per sub-agent: `●` when its model is set, `○` when it inherits, its name, its model, where it was read from, `project`, `global` or `library`, and its description, with its tools and references below. A file that cannot be read goes under
`broken` with a `✗` and the reason, and the command exits 1.

    tofu agents --json

prints one JSON document, whose `data` carries every field.

## Undo it

    tofu agents remove planner
    tofu agents remove --global reviewer

deletes the file `add` wrote and its line in `agent-models.yaml` in the same place. Without `--global` it only looks in the project, and with it only in your home. It prints the `add` command that brings it back.

A sub-agent tofu ships cannot be removed, since it is not a file of yours.
`tofu agents remove qa` refuses and names `tofu agents set qa` instead.

To undo a `set`, run `set` again with the model it had before, which the
`→ undo:` line names.
