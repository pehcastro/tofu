---
title: Output
description: The model is told to answer with evidence, reads less of each tool's output, and every tofu verb prints one shape and one JSON envelope.
order: 9
updated: 2026-10-04
---

The output contract covers three things:

- **The model's answer.** Every prompt ends with the same paragraph, which no
  setting removes: say what changed, name every file written, paste the real
  output of every command run, and say what couldn't be verified. The lead's
  report is a few lines, conclusion first. A sub-agent's report is checked
  against its brief's acceptance lines, and a line with no command and output
  is flagged.
- **What the model reads.** File listings are capped, large results are
  stored behind a handle, and shell output passes through a sift. See
  [Context](./context).
- **What tofu prints.** Every verb uses one shape: a title with a verdict,
  rows marked `●` `○` `✓` `⚠` `✗`, a `→` line with the next command, and an
  undo line after a write. `--json` prints one envelope.

## Why answers carry their evidence

**A claim with no command behind it is one the next reader has to redo.**
The contract makes the answer carry its evidence.

**Less read is less resent.** Capping file listings cut the tool result bytes
the model read by 88.4%, 54.3 MB down to 6.3 MB. The shell sift keeps 30 of 34
planted lines the model needed and still cuts 26.5% of shell output.

**One shape is one parser.** A script reads every verb's `--json` the same
way, and a person reads every page the same way.

## Using the output

- Pipe any verb's output to a script with `--json`.
- `NO_COLOR` turns colour off and `TERM=dumb` prints plain ASCII.
- Check a piece of text against the house style with `tofu sift --arm brevity`.

## Commands

```sh
tofu version --json
```

```json
{
  "tofu": "0.5.0-rc-fix24",
  "verb": "version",
  "ok": true,
  "at": "2026-10-04T03:53:49Z",
  "data": {
    "version": "0.5.0-rc-fix24+dev",
    "commit": "d8184775c9bfd4a1db16d70da3b3be4529b87418-dirty",
    "go": "go1.27.1"
  },
  "problems": []
}
```

```sh
printf 'You are right, this landed.\n\nShort line.\n' | tofu sift --arm brevity
```

```text
brevity  1/2 paragraphs kept · 2/7 words · 0ms · $0.000000
[sift:0 banned words: landed; it opens by agreeing]
Short line.
```
