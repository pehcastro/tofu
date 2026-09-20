# The catalog

Everything tofu ships as data lives here, one directory per kind. The directory is the kind, the file is the thing, and the path is the name. Nothing here is code and nothing here imports anything: `tofu/catalog` sits at the bottom of the import graph on purpose.

A kind is resolved in three layers, and the last one to mention a field wins that field:

1. `catalog/`, shipped inside the binary
2. `~/.boji/`, the person
3. `./.boji/`, the project

The merge is field by field, not file by file. A project file that says `use: allowed` and nothing else keeps every other fact from the layer beneath it. `tofu catalog` prints what loaded, from where, and every file it refused with the field that failed.

## models

`catalog/models/<provider>/<name>.yaml`

The path is the identity. A model is `provider/name`, one string, and it is the same string in a sub-agent definition, in `/model`, in `tofu run --model` and in the status bar. The provider is the vendor: `openai`, never `codex`.

| field | required | what it is |
|---|---|---|
| `subscription` | yes | the subscription that serves it, and a subscription file must exist for it |
| `use` | yes | `default`, `allowed` or `excluded`. A file with no `use` loads excluded, because tofu does not send a model nobody has ruled on |
| `reason` | when excluded | why, in the words of whoever decided |
| `window` | no | an extra quota bucket this model alone spends, on top of its subscription's |

Exactly one `default` per subscription, and a subscription with models has one.

Nothing else lives here. A file directly under `catalog/models/`, or a directory that is not a vendor, is refused by name rather than ignored.

## subscriptions

`catalog/subscriptions/<name>.yaml`

A subscription is an account you already pay for. It is not a provider: Codex is the OpenAI subscription and Claude is the Anthropic one. A quota window belongs here, because the window is spent by the account rather than by the model.

| field | required | what it is |
|---|---|---|
| `provider` | yes | the vendor it buys from, `anthropic` or `openai` |
| `wire` | yes | the name `tofu run --wire` and `tofu login` use, and the key the credential store is keyed by |
| `windows` | yes | the quota buckets every model on it spends |
| `not_models` | no | names the account serves that are not models, so discovery does not report them as unknown |

## roles

`catalog/roles/<role>.yaml`

A role is a name a call site asks for a model by. There are two, and both are read: `turn` is the turn you asked for, `child` is every child a turn spawns. Nothing ships bound, so a role with no file falls back to the default model of the subscription in play, and `tofu models` says which role is bound and which is falling back.

| field | required | what it is |
|---|---|---|
| `model` | yes | the model it binds, written `provider/name`, the same string everywhere else |

A file named anything but a role is refused by name rather than ignored, because a role nothing reads is a setting that silently does nothing. A role naming a model the catalog does not have is refused by name, and one naming an excluded model is refused with the catalog's own words for that exclusion.

The model names its subscription, so binding `child` to a model on another account is how one turn spends two rate limits. Binding `turn` to a model the running `--wire` does not serve is refused, naming the wire to run instead.

A session resolves its roles once, when it starts. Changing a file changes the next turn rather than the running one, because a record that cannot be reproduced is worth less than a setting that feels live.

## questions, policy, rules

`catalog/questions/<name>@<version>.yaml`, `catalog/policy/<name>@<version>.yaml`, `catalog/rules/<name>@<version>.yaml`

Loaded and validated by `internal/judge/question`, `internal/judge/policy` and `internal/rule`. `tofu catalog resolve <name>` prints a question set field by field with the file and line that decided each one. `tofu doctor` reports which policy decided, and `tofu rules list` reports which rule set did.

## Adding a kind

Four things, and none of them is a new mechanism:

1. A directory under `catalog/`, and the same directory name under `~/.boji/` and `./.boji/`. The layering is the same for every kind.
2. A contract: the field names, which are required, and what an absent one means. A required field added later defaults to the conservative value, because every file that already exists is missing it.
3. A loader that refuses a file by name and by field rather than skipping it, and that reports every refusal rather than the first.
4. A line in `tofu catalog`, so a person can see what loaded and what did not without reading the source.

Sub-agents, skills and hooks are the kinds coming next. Each one arrives as a directory plus a contract plus a loader plus a line, and nothing above it has to change.
