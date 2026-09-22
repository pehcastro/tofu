# The library

Everything tofu ships as data lives here. Nothing here is code and nothing here imports anything: `tofu/library` sits at the bottom of the import graph on purpose.

Two shapes live side by side and they answer different questions.

**What tofu may talk to** is filed by kind: `models/`, `subscriptions/`, `roles/`, `web/`, `questions/`, `changelog/`. The directory is the kind, the file is the thing, the path is the name.

**What tofu knows** is filed by domain: `dev/`, `qa/`, `general/`, `tools/<tool>/`. A domain holds `rules/`, `skills/`, `agents/` and `references/`, and a language directory inside a domain holds what is true only of that language.

Either shape resolves in three layers, and the last one to mention a field wins that field:

1. `library/`, shipped inside the binary
2. `~/.tofu/`, the person
3. `./.tofu/`, the project

The merge is field by field, not file by file. A project file that says `use: allowed` and nothing else keeps every other fact from the layer beneath it. `tofu library` prints what loaded, from where, and every file it refused with the field that failed.

## The domains

```
library/general/rules/         true everywhere
library/dev/rules/             writing code, any language
library/dev/go/rules/          writing Go
library/qa/general/rules/      testing, any language
library/qa/general/skills/     procedures a person or an agent follows
library/qa/agents/             sub-agent definitions
library/qa/references/         material any skill or agent in this domain may read
library/tools/shell/rules/     the shell tool alone
library/tools/shell/proxy.yaml the command proxy the shell tool runs behind, off unless a person turns it on
library/tools/fetch/rules/     the fetch tool alone
```

**`tools/shell/proxy.yaml` is a setting rather than a rule, and it is the one file under a tool directory that declares no domain.** `use` is `off` or `rtk`, and `off` means the turn loop holds no proxy at all: no process is started and no path is looked up. With `use: rtk` every bash command is offered to `rtk rewrite` before the tool gate sees it, so the gate, the person and the recorded row all carry the command that will actually run. `timeout_ms` bounds that one call.

**A directory is created only when something belongs in it.** There is no `library/dev/skills/` because no dev skill has been written, and no `library/qa/go/` because none of the twenty three QA skills read for TOFU-291 carries Go material. The shape without the content is worse than no shape.

**Every rule, every skill, every agent and every question declares its `domain:`, and a file that does not is refused by name.** The directory is not allowed to carry that meaning on its own, because a person adding a file has to say what it is for. A domain is `dev`, `qa`, `general` or a tool name, and under `tools/` the domain is the tool directory rather than the word `tools`.

**A reference is reachable by every skill and every agent in its own domain, and by nothing else.** A document naming a reference its domain does not ship is refused by name, so a reference is never silently missing at the moment somebody needed it.

## rules

`library/<domain>/rules/<id>@<version>.yaml`, or `library/<domain>/<language>/rules/...`

One directory holds two families of rule and `kind:` says which.

**A checker rule** is a check over a tree or a diff, read by `internal/rule`. It carries `id`, `domain`, `kind`, a `mode` of `shadow`, `enforced` or `off`, and either a `checker:` naming a builtin or, when `kind: measured`, a `measurement:` with the `source:` it was distilled from and the `evidence:` that paid for it. `tofu rules list` and `tofu rules check` read these.

**A threshold rule** is what a decision point reads, `kind: threshold`, loaded by `internal/judge/policy`. It carries `name`, `domain`, `kind`, `rule_version`, the `questions` set and `questions_version` it is asked against, a `mode`, a `sample_floor`, and a `thresholds:` block. `tofu doctor` reports which one decided and in which mode.

**A recorded decision names a threshold rule by `name@version`, never by path.** That is why a rule can move between domains without touching a single row in `.tofu/log`, and why old versions stay on disk: a recorded decision replays against the wording and the cuts that judged it.

## questions

`library/questions/<name>@<version>.yaml`

The wording handed to Jev, loaded by `internal/judge/question`. Each set declares its `domain`, and a set without one is refused by name. `tofu library resolve <name>` prints a set field by field with the file and line that decided each one.

## models

`library/models/<vendor>/<name>.yaml`

The directory names the vendor who built the model: `openai`, never `codex`. That is a fact about the model and does not change with who pays. **The identity tofu writes everywhere else, a sub-agent definition, `/model`, `tofu run --model`, the status bar, is a slug built from who pays rather than from the directory:**

- served by a subscription: `<subscription>/<name>`, for example `claude-sub/claude-opus-5`. The subscription carries the `-sub` suffix in its own file name, so it is spelled one way in the file name, in the `subscription` field, in the slug, and in `tofu login`.
- paid by a direct key, no `subscription` field in the model's file: `<vendor>/<name>`, for example `anthropic/claude-opus-5`. This is the one case where the bare vendor name is the correct identity, and it never collides with a subscription slug because a subscription name always carries the suffix.

The same model reached two ways, once on a subscription and once on a direct key, bills two ways and is filed as two entries so the slug always says which is paying.

| field | required | what it is |
|---|---|---|
| `subscription` | when a subscription pays | the subscription that serves it, and a subscription file must exist for it. Absent means a direct key pays instead |
| `use` | yes | `default`, `allowed` or `excluded`. A file with no `use` loads excluded, because tofu does not send a model nobody has ruled on |
| `reason` | when excluded | why, in the words of whoever decided |
| `window` | no | an extra quota bucket this model alone spends, on top of its subscription's |
| `context_tokens` | removed | **Gone since TOFU-304 on 2026-09-21.** A model file that carries one is refused as an unknown field. A context window is an upstream fact and a model file holds local decisions, so the two are no longer in one place. |

Exactly one `default` per subscription, and a subscription with models has one. A direct-key model carries no default requirement of its own.

Nothing else lives here. A file directly under `library/models/`, or a directory that is not a vendor, is refused by name rather than ignored.

**A context window comes from `models.dev` and from nowhere else.** Three layers: a snapshot embedded in the binary, a cache at `~/.tofu/model-windows.json` written by `tofu models --refresh`, and the rulings in `library/models/`, which no table ever touches. **`tofu models --refresh` is the only thing in the library that reaches the network**, and it is a verb rather than a background refresh, because a network call hiding behind a verb somebody ran for another reason is what would cost the library its no-network property.

An absent window still means tofu never compacts that model automatically, and a guess is still worse than none: too low compacts a turn that had room, too high sends a request the vendor answers with an overflow. 16 of 17 models carry one from the snapshot with no network. The one without is `codex-sub/gpt-reserve`, which no published table lists, and it stays without rather than getting a guess.

**Two of the five hand-read numbers were wrong and nobody could have known.** `claude-sonnet-4-5-20250929` was typed at 200,000 against a published 1,000,000, and `gpt-5.5` at 400,000 against 1,050,000. Both are excluded models so neither was ever sent, and both were wrong on disk for as long as they were there.

**A bare name is read, never written.** Session headers on disk from before this rule, and from before the money-based slug, carry a bare model name alone, sometimes with the vendor in a separate `wire` field. Reading one back matches it against every model's own name and resolves only when exactly one model in the whole library carries it; two models sharing a bare name resolve to nothing rather than a guess.

## subscriptions

`library/subscriptions/<name>-sub.yaml`

A subscription is an account you already pay for. It is not a vendor: `codex-sub` is the OpenAI subscription and `claude-sub` is the Anthropic one. The file name is the whole name, suffix included, and it is what `tofu login`, the `subscription` field of a model, every slug and the credential store all spell. A quota window belongs here, because the window is spent by the account rather than by the model.

| field | required | what it is |
|---|---|---|
| `provider` | yes | the vendor it buys from, `anthropic` or `openai` |
| `wire` | yes | the protocol it speaks and the name `tofu run --wire` uses. A wire is not a source, so `claude-sub` speaks `wire: anthropic` and the two are never spelled alike by accident |
| `windows` | yes | the quota buckets every model on it spends |
| `not_models` | no | names the account serves that are not models, so discovery does not report them as unknown |

## roles

`library/roles/<role>.yaml`

A role is a name a call site asks for a model by. There are two, and both are read: `turn` is the turn you asked for, `child` is every child a turn spawns. Nothing ships bound, so a role with no file falls back to the default model of the subscription in play, and `tofu models` says which role is bound and which is falling back.

| field | required | what it is |
|---|---|---|
| `model` | yes | the model it binds, written by its slug, `<subscription>/name` or `<vendor>/name`, the same string everywhere else |

A file named anything but a role is refused by name rather than ignored, because a role nothing reads is a setting that silently does nothing. A role naming a model the library does not have is refused by name, and one naming an excluded model is refused with the library's own words for that exclusion.

The model names its subscription, so binding `child` to a model on another account is how one turn spends two rate limits. Binding `turn` to a model the running `--wire` does not serve is refused, naming the wire to run instead.

A session resolves its roles once, when it starts. Changing a file changes the next turn rather than the running one, because a record that cannot be reproduced is worth less than a setting that feels live.

## Adding a domain, or a language inside one

A domain is a directory with something real in it and a `domain:` on every file. Nothing else is needed and nothing registers it: `tofu library` finds it by walking. A language directory inside a domain is created the first time a file belongs only to that language, and never before.

## Adding a kind

Four things, and none of them is a new mechanism:

1. A directory under `library/`, and the same directory name under `~/.tofu/` and `./.tofu/`. The layering is the same for every kind.
2. A contract: the field names, which are required, and what an absent one means. A required field added later defaults to the conservative value, because every file that already exists is missing it.
3. A loader that refuses a file by name and by field rather than skipping it, and that reports every refusal rather than the first.
4. A line in `tofu library`, so a person can see what loaded and what did not without reading the source.

Hooks are the kind coming next. It arrives as a directory plus a contract plus a loader plus a line, and nothing above it has to change.
