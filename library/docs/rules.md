---
topic: rules
title: Rules
summary: add your own rules for every project or for one, switch a rule off, and see which rules run
verbs: rules, reload
---

## What it is

A rule is one line of instruction the model reads, like "never write yaml
by hand". A rule may declare a `language`, `framework`, `scope`, `condition`,
`task` or `role`, and then reaches only the tasks that match. tofu ships its own set, and you can add yours,
switch one of tofu's off, or replace one with your own text.

A rule has an id, a short name in lower case letters, digits and
underscores, like `no_yaml`. The id is how you switch it off or remove it.
Each message you type is carried word for word when a session forks.

## Where it lives

tofu reads rules in three layers, each one over the last:

- the rules tofu ships, or the `library/` folder when the working
  directory has one, which replaces them
- `~/.tofu/rules/`: your global rules, in every project
- `.tofu/rules/` in a project: rules for that project only

Each rule is one file, `<id>@1.yaml`. When two layers carry the same id,
the later one wins: a project rule beats a global one, and a global rule
beats a shipped one. A rule switched off in a layer is gone from every
layer below it.

The prompt, `tofu rules list` and `tofu reload` all read the same three
layers, so what the list shows is what the model reads.

## Change it

Add a rule to every project:

    tofu rules add --global no_yaml "never write yaml by hand"

Add a rule to this project only, run from inside the project:

    tofu rules add no_yaml "never write yaml by hand"

From anywhere else, name the project with `--dir`. `add`, `off`, `remove`,
`list`, `check` and `index` all take it:

    tofu rules add --dir C:\code\shop no_yaml "never write yaml by hand"

The text is one line. An id that is already in the same layer is refused,
so nothing is overwritten by accident. Add `--replace` to write over it:

    tofu rules add --replace no_yaml "write yaml only through the generator"

Override a rule tofu ships, for this project or with `--global`
everywhere. An override says why, so `--reason` is required, and either
switches the rule off or replaces what it says:

    tofu rules off no_unit_test_after_code --project --reason "public SDK"
    tofu rules add no_unit_test_after_code "<new text>" --reason "public SDK"

The file names the rule and its version, `overrides: em_dash@1`, with
`reason:`, `by:` and `at:`. When tofu ships a newer version, the override
goes stale and the rule runs unchanged until you look again.

Every write prints what changed, `+` added, `~` switched off or `-`
removed, the file it wrote, and the command that undoes it:

    + rule no_yaml  ~/.tofu/rules/no_yaml@1.yaml
      → undo: tofu rules remove --global no_yaml

A refusal prints one `✗` line on stderr and the command to run instead,
and exits 1. With `--json`, a write prints one document instead.

A rule added with `--concern` is placed with that group in the prompt. The
groups are code_rules, the default, process_discipline, domain_knowledge,
task_shaping and identity.

## Check it

    tofu rules list

opens with `Rules · <n> run` and a count of `shadow` and `enforced`, then
a row per rule that runs, grouped by where it came from, `shipped`,
`library`, `global` or `project`, with its kind, its mode and the file for
your own: `●` enforced, `○` shadow, `✓` a rule with no checker, `-` a
rule an override switched off. An overridden rule shows its reason.

    tofu rules overrides

lists every override with its layer, reason and date, and marks a stale
one `⚠`. `tofu doctor` counts them and names the stale ones.

    tofu rules list --json

prints one JSON document, whose `data` carries every field.
`tofu rules check` and `tofu rules fired` print a `✗` row per blocked
finding and a `⚠` row per shadow one. `tofu rules index "<task>" [path...]`
says which rules fire for a task and why.

    tofu reload

counts the rules again from the working directory, and `/reload` does the
same in the app, after you edit a file by hand.

## Undo it

    tofu rules remove no_yaml
    tofu rules remove --global no_yaml

deletes a rule you added. Without `--global` it only looks in the
project, and with it only in your home.

    tofu rules restore no_unit_test_after_code

deletes an override, which brings the shipped rule back.

A rule tofu ships cannot be removed, since it is not a file of yours.
`tofu rules remove em_dash` refuses and names `tofu rules off` instead.
