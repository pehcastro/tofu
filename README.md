# tofu

tofu is a coding agent for your terminal, built as a single Go binary. You
talk to one lead model that plans the work, hands the writing to sub-agents
running in the background, and checks what they return before it answers.
It runs on the Claude and Codex subscriptions you already have, or on API
keys you bring.

## Install

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/pehcastro/tofu/release/scripts/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/pehcastro/tofu/release/scripts/install.ps1 | iex
```

The script downloads the latest release for your system, verifies its
checksum, and installs `tofu` into `~/.local/bin`. Run `tofu update` later to
move to a newer release. To build it yourself instead, see
[Build from source](docs/start/build-from-source.md).

## First run

Sign in with a subscription, then open tofu in a project:

```sh
tofu login llm claude-sub      # or: tofu login llm codex-sub
cd my-project
tofu
```

`tofu doctor` tells you if anything is missing and which command fixes it.
`tofu login classifier openrouter` adds the key for the safety classifier that checks
tool calls; without it tofu still runs, with that check off.

## What it does

- **A lead and sub-agents.** The lead plans and reviews; sub-agents write
  code in parallel, each limited to the files it owns.
- **Language agents with checks.** `go-dev`, `ts-dev`, `py-dev` and
  `rust-dev` are sent back until their language's typecheck and tests pass.
- **A rule library per language and framework**, which you can extend or
  switch off per project.
- **Fast checks in large repositories.** Typecheck and test runners stay
  running between edits instead of starting cold each time.
- **Your own Chrome.** tofu can read and drive browser tabs through a small
  extension, only in tabs it opened unless you say otherwise.
- **A record of every decision**, which `tofu why` explains and you can
  correct.
- **Scriptable.** Every command takes `--json`, and `tofu run` works a task
  without the interface.
- **Works with existing setups.** It reads `AGENTS.md`, `CLAUDE.md`,
  `.claude/agents` and `.claude/skills`.

## Documentation

The docs live in [`docs/`](docs/start/introduction.md): setup, models,
sub-agents, tools, the browser, the library, and the command reference.
`tofu docs` answers common questions from the terminal.

## Contributing

Issues and pull requests are welcome. To add a rule, an agent or a skill to
the library, see [Contributing to the library](docs/library/contributing.md).
To work on tofu itself, [build from source](docs/start/build-from-source.md).

tofu is 0.x: a minor version can change a command, a flag or a file format.
[CHANGELOG.md](CHANGELOG.md) lists what changed.

## License

MIT. See [LICENSE](LICENSE).
