# Changelog

The desk app, versioned on its own. A patch is fixes only; a minor adds features or changes what a person relies on: the files under `~/.tofu/desk`, the `tofu-desk` commands, or the tofu wire it speaks (`tofu.host/1`). Until 1.0.0 the minor carries breaking changes.

## [0.2.0] - 2026-10-16

The first build for daily use, installed at `C:\Users\Luiz\.local\bin\desk\`.

### Added

- Chat streams a real tofu session: prompt, tool rows, reply, the status bar shows context, account and quota.
- File edits tile lists what the agent created or edited, with line counts.
- Boardy screen, shown when `projectManagement` is `boardy` (Settings > Projects): Catch up, Need attention with open questions you answer by clicking, boards with Issues, Milestones, Projects, Cycles and Triage, list and board views, a ticket page, live updates when tofu moves a ticket, and Linear words when `boardFlow` is `linear`.
- Terminal tiles with select, copy, paste, find, padding and a shell badge; a tile opened from + takes the keyboard.
- Editor: preview tabs, two-pane split, multi-cursor, find and replace, folding, go to definition, a stripe on lines an agent changed.
- Notepad and Draw tiles from the + menu.
- Source control tile; undo restores a changed file.
- Settings: Keys rebinding, Editor, Terminal, Plugins with an Agent control tab (Ask, Allow, Deny per plugin command).
- `tofu-desk`: workspace, tile, tab, file, editor, screen, palette, theme, notify, status bar, clipboard (off by default), state and event verbs, Draw verbs, plugin commands with `--dry-run`.
- Chat tags: Card, Callout, Diff, Table, Chart; math; wide tables scroll.

### Known

- An agent appending to a file does not show in File edits (tofu side, TOFU-1492).
- Draw is not yet at tldraw's level: elbow arrows, label rewrap and arrow rebinding are unfinished.
- Settings search does not find a key by its name.
- A Boardy tab restored from the last session stays in the top bar while Boardy is off.
