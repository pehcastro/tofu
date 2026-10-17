# Changelog

The desk app, versioned on its own. A patch is fixes only; a minor adds features or changes what a person relies on: the files under `~/.tofu/desk`, the `tofu-desk` commands, or the tofu wire it speaks (`tofu.host/1`). Until 1.0.0 the minor carries breaking changes.

## [0.3.0] - 2026-10-17

The app gets its own name and look, a real git client, and the whole past of a session. Run `apps/desk/target_release/motion/tofu-desktop.exe`.

### Added

- Git screen (+ > Git, replaces the Source control tile): history graph of every branch, remote, tag and stash with commit details, a Changes tab with the diff, commit and amend, Undo that keeps your changes staged, Fetch, Pull and Push with a preview, confirm before discarding; shallow clones show their history.
- Title bar: the tofu cat in place of the word, white on dark and black on light, and a sun/moon theme toggle beside the bell.
- Boardy: group rows span the full width with + on the right, + opens a New ticket modal, the ticket page's Properties panel resizes by dragging its divider (220 to 480 px) and keeps that width across tickets, J and K step through tickets.
- Settings search finds a setting by its key, its choices (boardy finds projectManagement and boardFlow) and its description.
- Editor split: a button closes the second pane, the divider drags, the edited dot no longer fights the close button.

### Changed

- The app is `tofu-desktop.exe`; `tofu-desk.exe`, the command line that drives a running app, sits in `bin/` beside it. Release builds carry the light cat icon, dev and agent builds the purple one.
- Opening a session that is already running shows its whole history, its sub-agents with finished and failed state, and its file edits; the first message paints in about 250 ms.

### Fixed

- A Boardy tab closes when Boardy is switched off, and does not come back on relaunch.
- A git-ignored folder no longer blinks when opened in the file tree.
- Sub-agents of kinds the desk did not know (rust-dev) no longer vanish from the tile.

### Known

- A message typed in the desk while a turn runs is not yet proven to queue (undriven).
- Settings search takes about 18 ms per keystroke against a 16 ms target (DESK-422).
- Images in chat show as a badge only once tofu sends their paths (TOFU-1501).
- Draw is not yet at tldraw's level (DESK-376-1 in review).

## [0.2.0] - 2026-10-16

The first build for daily use, at `apps/desk/target_release/motion/desk.exe`.

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
