---
topic: rules
title: Rules
summary: add your own rules for every project or for one, switch a rule off, and see which rules run
verbs: rules, reload
---

## What it is

A rule is one line of instruction the model reads with every task, like
"never write yaml by hand". tofu ships its own set, and you can add yours,
switch one of tofu's off, or replace one with your own text.

A rule has an id, a short name in lower case letters, digits and
underscores, like `no_yaml`. The id is how you switch it off or remove it.

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

Switch a rule off, for this project or with `--global` everywhere:

    tofu rules off em_dash
    tofu rules off --global em_dash

Every write prints what changed, the file it wrote, and the command that
undoes it:

    added no_yaml to the global rules
    file: C:\Users\you\.tofu\rules\no_yaml@1.yaml
    undo: tofu rules remove --global no_yaml

A rule added with `--concern` is placed with that group in the prompt. The
groups are code_rules, the default, process_discipline, domain_knowledge,
task_shaping and identity.

## Check it

    tofu rules list

prints every rule that runs, its kind, and where it came from: `shipped`,
`global` or `project`, with the file for your own. A rule switched off is
not in the list.

    tofu rules list --json

prints the same as JSON.

    tofu reload

counts the rules again from the working directory, and `/reload` does the
same in the app, after you edit a file by hand.

## Undo it

    tofu rules remove no_yaml
    tofu rules remove --global no_yaml

deletes a rule you added, or the file that switched a rule off, which
brings the shipped rule back. Without `--global` it only looks in the
project, and with it only in your home.

A rule tofu ships cannot be removed, since it is not a file of yours.
`tofu rules remove em_dash` refuses and names `tofu rules off em_dash`
instead.
