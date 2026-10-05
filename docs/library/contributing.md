---
title: Contributing
description: How to write a rule, an agent or a skill, try it locally, and send it to the library as a pull request.
order: 5
updated: 2026-10-04
---

A contribution is a file in the library: a rule in `library/<domain>/rules/`
or `library/dev/<language>/rules/`, a reference in the matching
`references/`, or an agent in `library/<domain>/agents/`. It starts as a
local file in your own `.tofu` folder and goes upstream as a pull request to
[github.com/pehcastro/tofu](https://github.com/pehcastro/tofu).

## What earns a place

**A rule goes in when it changes what the agent writes.** One Rust rule,
`rust_bounded_recursion`, took a deep-recursion check from 1 of 6 runs to 3
of 3.

**Local first,** because the same file works in `.tofu/rules/` and in the
library, so you can use a rule for a while before proposing it.

**Notes say why, never where from.** A rule's `notes` name the failure it
guards against. The library's tests refuse a file that cites a source or
carries a date.

## From a local rule to a pull request

```steps
# Write it locally
`tofu rules add no_yaml "never write yaml by hand"` writes
`.tofu/rules/no_yaml@1.yaml`. Add triggers to the file by hand.

# Check where it fires
`tofu rules index "<task>" <path>` says which rules fire and why.

# Move it into the library
Fork the repository and put the file under `library/`. From the repo root,
the `library/` folder replaces the built-in rules, so tofu uses yours before
you build. A new folder also goes in
[`library/embed.go`](https://github.com/pehcastro/tofu/blob/develop/library/embed.go).

# Check it reads
`tofu library` must end with `✓ nothing refused`, and `go test ./library/`
must pass.

# Open a pull request
Include the runs with and without the rule that show it changed the result.
```

## Commands

```sh
tofu rules index "fix the yaml loader in config/load.go" config/load.go
```

```text
Rules index · from the project                                  ● 32 of 157 fire

  task      fix the yaml loader in config/load.go
  paths     config/load.go
  concerns  code_rules, process_discipline, safety, output_shape, tool_guidance
```
