# Changelog

Kept by hand, in the shape of keepachangelog.com, and versioned by semver.

**`.local/boji/diagram.html` is updated in the same edit as every entry below.** It draws what runs where, keeps a version selector so an older shape can be read back, and marks in green what changed in the selected version. It exists because too much moves in a night to hold in one head.

Tofu is personal and not released, so the public interface that a version promises is the command line and the file formats: the verbs and their flags, the exit codes, the catalog schema, the ledger row schema and the policy schema. A version says what changed for someone driving the binary or reading its files, not what changed inside it.

The minor number carries breaking changes, which is what 0.x means, and **there is never a 1.0.0**. The owner decided that on 2026-09-19: this stays 0.x forever, so the promise the version makes is the one 0.x already makes, that anything can break on a minor bump.

## Unreleased

## 0.4.13 - 2026-09-23

A file tofu reads cannot crash it, and you can tell it to ignore the house rules.

### Added

- **`tofu rules list` shows a mode only where the mode decides something.** A threshold rule's mode is read and is shown; a rule with no checker and no off setting had a mode printed beside it that nothing consulted, which read as a setting you could change.

### Fixed

- **A ledger file, a subscription filename, a credential row, a cached answer and a zero valued message role could each crash tofu rather than being refused.** All five were reachable from a file or a reply rather than from code, none had ever been reached: 2,780 recorded decision rows, 3 credential rows, 75 cached answers and two subscription files were checked and every value was one this build knows. Each is now refused where it is read, and the row that fails says why instead of disappearing.
- **A credential whose provider tofu cannot read no longer disappears from the list.** It stays, marked disabled with the reason, so an account you cannot use reads differently from an account you never had.

### Added

- **`tofu run --no-instructions` sends no instruction file at all**: neither the nearest `AGENTS.md` or `CLAUDE.md` at or above the working directory, nor your personal `AGENTS.md` or `CLAUDE.md` in your home directory. The default is unchanged and still sends both. Until now a run inside a repository carrying instructions written for another tool had no way to say no, and the benchmark arranged it by writing empty files into the tree. `--show-prompt` says `instruction files: off by request` under the flag, so a missing block reads as a choice rather than an empty directory.

## 0.4.12 - 2026-09-23

You choose what runs, and you can point at what was said.

### Added

- **`tofu run --effort <level>`** sets how hard the model thinks: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`. The vocabulary is the vendors' own, not a new one. The anthropic wire takes `low` through `max` and refuses `minimal` by name rather than picking a neighbour; the codex wire takes all seven. The openrouter wire sends no level, so `--effort` with `--wire key` is refused instead of being dropped.
- **A `[quote#abcd]` reference resolves to the turn it names.** `/quote` has inserted one since 0.4.9 and nothing could read it back. A tool now returns that turn's words, so the model cites rather than paraphrases. An id matching two turns, and an id matching none, each come back with their own message asking for the reference again rather than quoting the nearest turn. A turn recorded before event ids existed is quotable: 856 of them are.
- **The crew view shows what a child is calling while it is calling it.** It carries when the child started, when it last stepped, how many steps it has taken, and the names of its last five calls. It said "no tool call yet" for the whole life of a running child before.

### Fixed

- **`tofu shell kill` returned while the tree was still running.** Terminating a job is asynchronous and nothing waited on its members, so for about 16 milliseconds after kill reported success the children still held the port and the log. An immediate restart under the same name hit "address already in use".
- **A process the shell spawned before it joined its job escaped kill for its whole life.** The gap was about a third of a millisecond against a first child at 39, so it never fired on an idle machine. Held open on a real condition it fires every time: 40 of 40 rounds, 20 of them still running when kill returned. The shell is created suspended and resumed only once it is a member, so nothing it spawns can predate its membership.
- **The crew view kept its own copy of the crew, with its own clock.** The elapsed time a person read was measured from a timestamp the view took, not from when the child started, and three of its six states could never appear.
- **The model picker offered a model a hook forbids.** Its default selection was on one of the two banned models, and nothing checked the excluded flag at the point of picking.
- **Eleven shipped rules declared a mode that could not do anything**, and one of them said `enforced`. A mode only chooses for a rule that names a checker. Three rules carry no checker and no longer carry a mode, and `enforced` is refused by name on the kinds that cannot have one.

### Changed

- **A turn now thinks at `medium` by default.** It ran at none until now, which nobody chose: it was what an unset field did. Every subscription turn, in the terminal interface as well as on the command line, now carries a thinking level, and a turn costs more output tokens than it did.
- **The model picker changes the model, not only the subscription**, and carries the thinking effort beside it, chosen with the left and right keys, because the model and the effort are one choice. It offers only the levels the picked subscription's wire accepts. The pick lasts until the app closes.
- **The anthropic request carries `output_config.effort`** and the `effort-2025-11-24` beta with it. The request field was a bool that nothing ever set and is now a level.

### Note

**The benchmark was understating tofu's own token use by 62 percent.** The tofu arm counted prompt plus completion and ignored cache entirely, while the claude arm counted all three into one column, so the two sat in the same table under two definitions of billed input. Every tofu against claude token figure produced before today was wrong in tofu's favour.

**The three benchmark arms are asked the same thing now.** All three run at medium, and the runner says so on its own last line.

**Three corpora leak and none was repaired.** In one, three of four questions name their own answer inside their own prompt, so a regular expression scores three of four: that package is parked, because the recorded sessions yield seven usable turns carrying two distinct tasks against a floor of thirty. A planted state in another is recoverable from a substring. Each is named in its own provenance rather than quietly fixed, because the leak is the evidence that a figure was wrong.

**Nothing on the report page cites a figure a withdrawal struck.** The build refuses it, and a quotation that is a table row strikes every row of that table.


### Added

- **`tofu run --effort <level>`** sets how hard the model thinks: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`. The vocabulary is the vendors' own, not a new one. The anthropic wire takes `low` through `max` and refuses `minimal` by name rather than picking a neighbour; the codex wire takes all seven. The openrouter wire sends no level, so `--effort` with `--wire key` is refused instead of being dropped.

### Changed

- **The model picker changes the model, not only the subscription.** Picking `claude-sub/claude-sonnet-5` runs the next turn on that model, through the same check `tofu run --model` goes through, so an excluded model and a model on a subscription the turn cannot reach are still refused. The pick lasts until the app closes.
- **The picker carries the thinking effort beside the model**, chosen with the left and right keys, because the model and the effort are one choice. It offers only the levels the picked subscription's wire accepts, and an effort the other subscription refuses falls back to `medium` when the pick moves there.
- **A turn now thinks at `medium` by default.** It ran at none until now, which nobody chose: it was what an unset field did. Every subscription turn, in the terminal interface as well as on the command line, now carries a thinking level, and a turn costs more output tokens than it did.
- **The anthropic request carries `output_config.effort`** and the `effort-2025-11-24` beta with it. The request field was a bool that nothing ever set and is now a level.

## 0.4.11 - 2026-09-23

Tofu acts on what it measured. A bash result arrives cut, and one file says what decides what.

### Added

- **A bash result reaches the model cut**, with a line at the end saying how many of how many bytes went and which method decided. Over 170 recorded bash results in 35 sessions the free arm saves 49.1 percent of bytes, concentrated in a few large outputs: the median session saves nothing and the best saves 74.7 percent.
- **`tofu run --sift <arm>`** picks the arm by hand, `free` or `judged`, where the method table otherwise decides. `--help` prints what each arm costs, read from the table at print time.
- **`library/decisions/methods@1.yaml` names the method for every decision point**, one line each, reading judged, cheap or unwired. Changing which method decides a point is changing one line. **Unwired is a stated answer with a reason, not a default.**
- **`tofu rules fired [date]`** reads back the record tofu already writes on every rule that fires. 223 fires were on disk with no way to look at them.
- **`tofu usage --history`** shows every recorded quota reading with the moment it was captured. 28 were on disk and only a benchmark read them.
- **`/models` opens the model picker**, and the pick changes the wire the next turn runs on. It offers only the subscriptions the turn can actually run, and an excluded model cannot be picked.
- **`edit` takes a Go symbol**, so replacing a function no longer means reproducing the old one character for character. Any edit that would leave a Go file unable to parse is refused with nothing written.
- **A decision row says whether a turn or a benchmark wrote it.** 1,104 of 2,756 recorded rows are bench measurements, which is 40 percent, and nothing could tell them apart before.

### Fixed

- **A fresh clone did not build.** The method table and its parser were never committed, and `library/embed.go` embeds the library.
- **Two agents could be given the same path.** The collision check and the permission check were two implementations of one glob language and disagreed on whether a subtree glob covers the directory it names. There is one matcher now, and it narrows: more spawns are refused, none newly allowed.
- **The model picker offered a model a hook forbids.** Its default selection was on one of the two banned models, and nothing checked the excluded flag at the point of picking.
- **`tofu reload` named two loaders that do not exist.** It said it had skipped skills and hooks. There is no loader for either, so it says what it actually re-read.
- **`frame` was a verb the usage text never mentioned.** A test now compares the usage text against the verb table.
- **The benchmark understated tofu's own token use by 62 percent.** The tofu arm counted prompt plus completion and ignored cache entirely, while the claude arm counted all three into the same column, so the two sat in one table under two definitions of billed input. Every tofu against claude token figure produced before today was wrong in tofu's favour.
- **The benchmark measured repeat two on top of repeat one.** The runner never staged the task's seed between repeats.

### Changed

- **The three benchmark arms are asked the same thing.** Claude runs at `--effort medium` and codex at `model_reasoning_effort=medium`. **Tofu has no effort flag at all and its request leaves thinking unset**, so it runs at none, and every row says so rather than leaving a reader to find out.
- **The report page reads what decides what.** It said 0 of 8, hand written; it says 3 of 9 now, derived from the method table, and the build fails if the page and the table disagree.
- **The link-only rule lives once.** It existed twice, character for character, in the fetch path and in the sieve. The benchmark was measuring the second copy and reporting what the first already does.

### Note

**`page_sift` is measured and not wired, and that is a result rather than a gap.** Its cheap arm elides 0 further bytes on all 24 recorded pages, because `web.Reduce` already strips the same rows at fetch time, where `Reduce` itself takes 11.69 percent. A second pass of one rule is not a decision point.

**The shell sieve acts with no calibration lock, so `tofu doctor` reports shadow while a turn cuts.** The rule's threshold is a prior and not a fit. The two will be made to agree.


## 0.4.10 - 2026-09-22

Tofu tells the model which rules apply to the work, and can show you exactly what it sent.

### Added

- **The system prompt is composed from the rules that fire for the task.** A rule declares which of ten concerns it is, five of which are always on, and the other five fire on the language of the paths, the directories touched or the verb the task names. A rule that fires and carries no text fails the run rather than being dropped quietly.
- **`tofu run --show-prompt` prints the prompt a turn would use** before it runs: every part, its concern, its size in bytes, and the rule file it came from, then every held-back rule with the reason it did not fire.
- **`tofu rules index "<task>" [paths...]`** answers the same question without running anything.
- **`tofu why <id>` prints the state a decision was made on**, reading back a state that was written to its own file. A missing state file, a state file outside the ledger, and a state that is there now read differently from each other.
- **`projectInstructionsCap` is a setting**, 32 KB by default, and **tofu tells you on screen when your instruction files are cut** rather than only telling the model.

### Fixed

- **Your instruction files were cut at 16 KB and nothing said so on screen.** The cap is 32 KB now, which is what the one other harness that caps this uses, and a 17 KB `CLAUDE.md` reaches the model whole.
- **Every rule cited a source under a directory that is not in the repository.** Thirteen citations pointed at a scratch path on one machine, so a fresh checkout had thirteen dead references. Every rule now cites a file under `library/`, and a test refuses one that does not.
- **The composed prompt carried the absolute path of every rule file on the machine it ran on.** It names the rule now, and `--show-prompt` still prints the path for a person debugging.

### Changed

- **A rule carries the text a model reads, separately from the notes a maintainer reads.** Before this, nine of ten rules had only maintainer notes, so a prompt told a model that a rule "wraps internal/crew.Matches" rather than telling it not to write outside its paths.
- **`bench/report/index.html` answers three questions**: is tofu better than claude and codex, is this version better than the last, and where does a judgment beat the cheaper way. The first two say what they would need rather than filling a table from one sample.

### Note

**Nothing tofu judges changes what tofu does.** All nineteen shipped rules and every decision point read `mode: shadow`. The report page says so on its own front tab: 0 of 8 measured decisions are switched on.

**Of the six decisions with a cheaper method measured beside them, Jev wins four and loses two.** `shell_sift` is the clearest win at 1.50x and switching it on would cost a median $0.00054 a session, one cent in the worst recorded session, and 838 ms on a shell tool call.

**rtk was measured for the first time.** In front of Jev it buys a 29 percent cheaper call and pays 5 of 34 needles for it. Neither rtk arm beats Jev alone, because rtk has no filter for 21 of the 34 recorded commands.

## 0.4.9 - 2026-09-22

Tofu runs on the account that has room, and every number it reports is one you can check.

### Added

- **Tofu picks an account instead of refusing.** With more than one account on a subscription it chooses once, at session start, by how much headroom is left across the windows that bind, and it stays on that account for the session. `tofu login --disable` is no longer the way to get past a second login.
- **An account that runs out mid-session moves rather than stopping.** The session forks onto the next account carrying its handles, and the screen says which account it moved to, why, and what the move cost. A child picks its own account and leaves the parent's drained one alone.
- **`tofu rules index "<task>" [paths...]`** says which rules would fire for a task and why each held-back rule did not. A scope that reached nothing reads differently from a condition that did not match, and a task naming no paths is answered rather than refused.
- **`/links` lists every link the conversation carried**, newest first, with a count when one appeared more than once. Only `http` and `https` are shown, and a link with credential-shaped query parameters is shown with the parameters cut.
- **`/quote` inserts a reference to a past turn**, `[quote#39cl]`, and nothing else. The reference is an id into the record rather than pasted text, so it cannot go stale and does not cost its tokens twice.
- **Every quota reading is written to `.tofu/quota/<date>.jsonl`** when tofu polls a vendor, carrying the credential row number, the fetch time and each window's id, use and reset. No token, account id or email is ever in a row.
- **`bench/report/index.html` is one page over every dated report**, with the headline number, the arms, the sample size and the skips, and it opens from disk with no network.

### Fixed

- **An account with room read as spent.** A vendor reports per-model windows beside the account windows, and one of those sitting at its cap made the whole account unusable. A window binds only when it is not scoped to a model, so an account with most of its five-hour window free is usable and is ranked properly.
- **The token estimate read 32 percent over on Codex.** One constant served two wires that tokenize differently. Measured over the recorded corpus, the median error across both wires falls from 18 percent to 7 and the worst from 47 to 20.
- **A recorded step's occupancy described a request nobody sent**, because it was measured after that step's tool results were appended. It is now the request as sent, and a recorded row keeps every band: the largest one was silently decoding as zero.
- **The context bar showed the number the fork decided on** rather than what the request occupied. They are different quantities and the bar now shows the one a person watching a turn can act on.
- **A file path read back out of the ledger, and a session handle typed on the command line, were both followed without a guard.** A path that leaves its root is refused, and the refusal cannot be mistaken for a missing file.
- **A truncated tool result said only that something was missing.** It now says how many bytes were dropped, and when the whole output could not be stored it says that too, rather than leaving a hole with no explanation.

### Changed

- **A rule declares which of ten concerns it is, and the declaration is required.** Five concerns may never be conditional, so a rule that is about output shape, safety, the environment, tool guidance or the report format is refused if it declares a trigger at all. Two shipped rules were firing on the wrong trigger and one of them fired on a task naming only `CHANGELOG.md`.
- **A rule fires on the language of the paths, the directories touched, or the verb the task names**, rather than on a path glob standing in for all three.
- **The harness reports a median and a range over repeats rather than a mean.** A comparison where either arm has fewer than two passing repeats prints `not separable`, and a task that passes a gate on one repeat and fails on another is named unstable and ranks no arm.

### Note

**Three measurements in this release returned no, and that is the point of having them.** A moved cache breakpoint caches exactly as much as the old one, because the vendor looks back twenty content blocks and a turn here adds at most eight. The recorded corpus cannot separate a thrift arm from a shaping arm: six of 109 sessions carry both, and 22 sessions are one fixture written to produce reading. And the account picker cannot be scored against any arm yet, because until this release nothing ever wrote a quota reading down.

**None of the 25 reports under `bench/` is built by rerunning its own code.** Every one is built from the text of its own markdown, and every one now says so on its page.

**A recorded session written before this release keeps the old meaning of its occupancy** under the same key, and nothing in a row tells the two apart. Sessions recorded before event ids existed get a derived id when quoted, which resolves the same way every time but is not the id the record carried.

## 0.4.8 - 2026-09-22

`catalog` is `library`.

### Changed

- **The `catalog` directory is now `library`, and `tofu catalog` is now `tofu library`.** Two other tools already call the same thing a catalog, and a shared word made this look like a copy of theirs when the contents are ours. `library` also says the right thing: a place things are looked up from, rather than a list of what exists.
- `tofu rules list` and `tofu rules check` take `--library` where they took `--catalog`.

### Deprecated

- **`tofu catalog` still runs and tells you the new name.** It goes at the next minor bump.

### Note

A decision recorded before today names its rule file by a `catalog/` path in the sentence `tofu why` prints. **Nothing resolves a path from that sentence**, so old decisions replay unchanged; the only effect is that `tofu why` on a row from before this release names a directory that no longer exists.

## 0.4.7 - 2026-09-22

More than one account, and a screen that stops lying about which one.

### Added

- **`tofu login --status` is a listing rather than a line.** Each subscription is a heading, each account under it carries the email it belongs to, the plan when the vendor reports one, and how full each quota window is. `--redact` masks the account when you are sharing a screen.
- **A second account for the same subscription is stored and reachable.** Signing in again no longer overwrites the first one. When two are usable and nothing says which, tofu refuses and names both by number rather than picking by accident, and the listing says so with the command that sets one aside.

### Fixed

- **A second Codex login used to overwrite the first.** A Codex credential stored without naming its account, so both rows collided. A login names its account now, and the row already on disk is repaired the first time tofu opens the store.
- **With two accounts on one subscription, quota was polled for neither.** Each account is polled for its own windows, and a window is remembered against the account it belongs to rather than against the vendor.
- **`tofu usage` said no credential was stored while listing two of them**, whenever every window was spent.
- **The account was printed in full in the settings pane and masked in the listing.** One rule now, and the mask tells two accounts apart instead of rendering them identically.
- **A command that succeeds and prints nothing is no longer sent as an empty message**, which some vendors refuse. An empty success and an empty failure also read differently now, so the model can tell a command that worked from one that broke.
- **A turn that stops itself keeps its last word.** Tool calls left unanswered when the loop guard tripped made the final request malformed, so the explanation of why the turn stopped was the thing that got lost.

### Changed

- The token benchmark replays against a fixed commit instead of the working tree, so its numbers measure the cap rather than yesterday's edits.

## 0.4.6 - 2026-09-21

A rule file with one bad number no longer turns the gate off, a failed request is tried again, and the mouse works.

### Fixed

- **A tool gate rule that cannot be satisfied now refuses to start a turn instead of running every call unjudged.** A missing key and an unusable rule were one branch, and both left the gate off behind the same banner. They are two now and they read differently on screen. **There was also no range check at all**, so a threshold of 1.85 on a zero to one scale loaded cleanly and asked the model anyway.
- **Both subscription wires retry.** One overloaded answer used to end a turn five steps in. The retry window is the connection, never the stream, so a retried request can never repeat text already on your screen. **A step can now put five requests on the wire, and a retried one may still bill.**
- **A one letter answer no longer approves a gate ask.** The answers are `1`, `2` and `3`. Typing a sentence that began with `a` under an open ask used to allow the call on its first character.
- A pale column down the left of the composer. It was a border glyph painted over the tint, and the tint is meant to be the only edge.
- An id typed from the transcript reaches the work view. It was matched against the start of an id where the screen draws the end.

### Added

- **The mouse.** The five tabs and any trace id are clickable. Everything else on the screen still selects with a drag, and a drag that starts on a tab selects instead of switching.
- **A gate ask says what tripped it in words**, using the wording the judge itself was given rather than a scale invented here. A decision recorded before this still prints its numbers.
- **A command proxy, off by default**, set in `catalog/tools/shell/proxy.yaml`. It rewrites a shell command before the gate sees it, so the ledger records the command that actually ran. A project's own filter file is refused, and a proxy that crashes falls back to the raw command.
- **`README.md`.** The repository now says what tofu is in its first line, that it is personal and not released, which judgments lost to their free arm and are switched off with the numbers that say so, how to build and drive the binary from a fresh checkout, and where in `bench/` the numbers live. There is still no `LICENSE`, so the default is all rights reserved.

### Measured

- A pasted screenshot costs about 1,765 tokens. 190,476 bytes at 1536 by 850, measured against the same turn without it and cross-checked against the vendor's own area rule. **File size says almost nothing about the cost.**
- A recorded session runs a median of 7 tool calls and at most 55. 1.5 percent of tool result bytes come off losslessly, and `glob` is 92 percent of the corpus by size with none removable.
- Fifty three production failures published by another harness were checked against this tree: five were real and are named in `.local/boji/planning/doing/known-failures.md`.

### Removed

- **The `grep` tool is gone. `search` is the only way to find text.** Over a tree of 13,140 files the two tools took 48.5 s and 37 s for the same query, and returned 133 MB against 17 KB. **The removal is about the bytes, not the seconds:** a tool that hands back 133 MB spends a context window on one call. `grep` could not be capped, because every cap tried cost half the correct answers on a thirty question bench, so it goes rather than shrinks. `search` now says in its own description that it finds text and that there is no grep tool, so a model does not reach for what is not there.

### Changed

- **`tofu search` reports what it scanned.** A call says how many files it looked at and how many it returned, and stops after 1,200 candidates rather than reading a whole tree. The cap has never fired on a real question.

## 0.4.5 - 2026-09-21

What is happening right now has its own line, and five things on screen have their own colour.

### Added

- **A progress line under the running summary.** The summary carries the counters and the line beneath it carries the one call happening now, with a spinner while it runs. It replaces itself, so a turn with twelve tool calls is still two lines. **The sub-agents view draws the same line for a child.**
- Five things that were all the same dim now read as themselves: a tool call, a shell command, a file path, an event id, and a finished call.
- Whether the running line counts shell calls separately is a setting.

### Changed

- **The separator above the composer is gone.** The tint already says where the input begins and two marks for one boundary is one too many.
- **The composer has a blank row above and below its text, inside the tint**, so it reads as an area rather than a strip with words in it.

### Fixed

- **A pasted image deleted from the message was still attached.** Two separate paths carried it: the chip list under the composer, and a list of files on disk that never looked at the message at all. **Both now keep only what the message still refers to.**

## 0.4.4 - 2026-09-21

He ran it and wrote down what it drew.

### Added

- **`tofu frame` renders the interface to standard output and exits**, at any width and height, with `--plain` to strip the colour. The interface can now be read without being run, which is how three of the fixes below were found.
- A progress line is not here yet. What is: the running line now reads `· (12) tools · jev 6 · shell (1) · 44s · [#c11]` and never names a tool.

### Changed

- **The clock starts when you press enter and stops when you have the final answer.** It no longer restarts on a phase, a request or a tool call, and only the word beside it changes. `requesting` appears once, before the first response of a message, and never again in that turn.
- **A short id is the last six characters of the real one, not the first.** Every session id begins `turn-` and a hex timestamp, so for months every id on screen was the same six characters: `#turn-1` under everything.
- **The running line counts tools rather than naming them.** A shell keeps its own count, because a process that outlives its call is a different kind of thing.
- Headings render as headings. A second level heading printed its own hash marks, and every markdown fixture was a short handwritten string, so nothing caught it. The fixture is now a real recorded answer of 583 words.
- Brackets mean a thing can be activated, `[1] chat` and `[#c11]`, and the whole label is the target, not the bracket.
- A finished turn reads `cooked for 44s`.
- **The arrow keys belong to the input and never move the transcript.** The wheel, `pgup` and `pgdown` scroll. No key does both.
- The composer's tint covers every row of it, at every colour depth.
- A shell command draws in its own colour rather than the same dim as every other tool call.

### Fixed

- **The vendor's own tool-use id was on screen.** `#toolu_` was Anthropic's identifier for a call, printed directly. The interface mints its own id per call and pairs it with the result.
- The placeholder's first character looked like a leftover letter you could not delete. It was the terminal cursor drawn as a block over it, now a bar.
- The greeting stayed at the top of the transcript for the whole session. It goes at the first message.
- A model named by a subscription is drawn that way everywhere, including the frame header, which still read `openai/gpt-5.6-sol` for a model a Codex subscription serves.
- Three placeholder examples, one picked per session, instead of an instruction that repeated the hint line below it.

## 0.4.3 - 2026-09-21

The interface he designed, and the first tool a judgment clearly wins.

### Added

- Five views, on five digits: chat, work, file edits, sub-agents and shells. **Chat folds every tool call into one running line and never draws a row per call.** The command, the result size and the gate verdict all move to work, which draws each call whole with its arguments and its output. `ctrl+o` jumps to work rather than expanding the transcript.
- **Every event carries an id**, drawn as `#a3f9c1`. Typing an id jumps to that event in work. The record holds a full uuid and the screen prints the first six; an id that matches two events resolves to neither rather than picking one.
- File edits is a diff feed with a sidebar of the agents in the session, active and finished separately. Each change names who made it, where, when and its id. **A path is written as a terminal hyperlink**, so it opens in whatever editor the system already hands it to. Nothing configures an editor.
- Shells: any process an agent started that outlives its call, a dev server, a build, a test run. `tofu shells list`, `tofu shells log <name>` and `tofu shells kill <name>`, and `k` kills from the view. The registry is a real directory, so the command line and the interface see the same processes.
- Settings persist. Global and project, project wins, and the view names the file a value came from. A change is on disk before the next keystroke. **A warning appears only when a setting that truly needs a restart has changed**, from a snapshot taken when the screen opened. Typing filters, and a row whose value differs from its default is marked. `tofu settings get|set` reads the same table, and `tofu reload` re-reads rules.
- **`tofu frame` renders the interface to standard output** at any width and exits, so it can be read without being run. `--plain` strips the colour.
- A models picker grouped by whatever serves each model, and an update check that runs in the background, never blocks, and stays silent when there is no network.
- Input history on up and down, restoring the draft. A pasted image becomes a numbered chip where it was pasted, and the sent message draws a tree naming what went with it.
- Markdown renders while the answer is still streaming. A line already drawn does not change shape when the next delta arrives.
- `tofu shells` and the settings verbs aside, `konst` gained a memory ceiling and a worker count for mutation runs, and a cap on the diff table.

### Changed

- **A model is named by what pays for it.** `claude-sub/claude-opus-5` when a subscription serves it, `anthropic/claude-opus-5` only when a direct API key does. The same model reached two ways bills two ways and the prefix is the only thing that says which. The suffix is derived from the subscription's own name, so a new one needs no extra field.
- **The turn has no decision cap.** It ran to forty gated tool calls and stopped mid-sentence. Any cap is a setting now, and zero means none. `--max-decisions` is refused by name.
- `glob` returns at most 300 paths and says how many matched. Uncapped it returned 6.3 MB for the pattern `*`, and across the recorded corpus that one tool was 94 percent of every byte the model read. **Capping it cuts 93.1 percent of all tool result bytes.**
- The bottom bar is two rows: context as a value over a value with a bar, both quota windows each with its own percentage and reset, and read, written and cached as three separate numbers. **The reset is the last thing dropped as the terminal narrows**, not the first.
- The top row reads path, branch, `source/model`, the session name with its id, and the session clock. No version string, no arrow.
- A blank line separates a person's message from the answer above it, the break between two turns is larger than any break inside one, and the activity block has a gap above it.
- Sub-agents carry a state rather than a tab each: working, waiting for an answer, in review, parked, errored, finished.

### Fixed

- **A mutation run reached 30.6 GB on the owner's machine.** The runner allowed four test binaries at once at thirty times the clean run with no memory bound, and killing it killed only the wrapper. It now runs one at a time inside a job object with a 4 GiB ceiling the kernel enforces, and cancelling kills the whole tree.
- `internal/transform`'s diff built a table the size of one file's lines times the other's, with no bound. A large enough diff reached it without any mutation at all. It is guarded before the table is built, so no mutation to the surrounding loops can defeat the guard.
- The package's own test binary never finished when run outside `go test`, because it hashed the working directory rather than its own. It resolves its own directory now and exits in half a second.
- A killed process was sometimes recorded as having exited on its own, from a race between the kill and the waiter. Both now agree through one lock.
- `grep` and `search` walked the whole tree reloading every ignore rule at every directory. The walk over a 3.3 million line tree fell from parity with a full crawl to about a second.
- Four wall-clock tests asserted the worst frame of a run and failed whenever the machine was busy, at 119 ms against a 16.7 ms budget while the average stayed at 1 ms. They assert the median and print the worst.
- Two published bench reports carried the owner's email, username and machine paths in transcript rows. Scrubbed, and the transcript column that carried them is gone.
- The transcript no longer sinks to the bottom of an empty screen, and the fold line stops alternating between grey and blue on every tool call. It carries the tool count, the jev count and the elapsed time, and no longer a byte total.

## 0.4.2 - 2026-09-21

It stopped making the same mistake twice, and it stops itself when it starts.

### Added

- The model can state a plan, mark one item running and mark it done, and the plan draws in the session view directly above the running row without moving the transcript. One item runs at a time and a second is refused. An item is named by its words, never by an index.
- `tofu sift` reads a shell result against the task that asked for it and marks what is still worth reading. It is in shadow: nothing is removed and the model is given the output whole. Standard error, the chunk carrying the exit status and the first and last chunk of standard output are never candidates.
- A policy file may declare a `schema` other than the gate's, and its thresholds load with it. A threshold that is not a number fails the load with the file and the line rather than reading as zero.
- `tofu check` writes a ledger row carrying the fingerprint of the command it judged, so a judgement made by hand at the command line is found as precedent by the same call inside a run. A replayed row carries the fingerprint of the row it replays.
- A turn stops itself when the model calls the same tool with the same arguments and gets the same result three times inside six calls, and it says which tool, with which arguments, and how many times. Before this it ran to the forty step cap and stopped without explaining anything.
- The prompt carries the working directory, today's date, the platform, whether this is a git repository and the branch, and it reads a `CLAUDE.md` or an `AGENTS.md` walking up from where you typed `tofu`. The nearest file wins. The text is capped and says which file it cut and by how much.

- `fetch` drops a list or table row whose whole content is a link, because that is navigation, and the result says how many units and how many bytes went. A code block and the page title are never dropped. Over three large reference pages this removes 16.2 percent of the text, and 19.9 percent of the Node file-system reference, without losing anything that answered the question.

### Fixed

- `tofu doctor --json` carries a `schema` field for a point that is not the gate, and no longer reports a policy it cannot read as making the binary unusable.
- An image pasted before the first send of a session lands in that session's directory rather than in a different one, and the session body records the file, its size and its format. A paste in a directory with no recorded session works.
- The request that writes a turn's last word now carries the same tool definitions as every other request, so a turn that ends at a cap no longer pays for an uncached prefix. That block is 5,960 tokens, 62 percent of the cached prefix, and it was being bought again on every capped ending.
- A project's own `CLAUDE.md` no longer sits inside the cached part of the prompt, so opening a second project stops invalidating the cache of the first.

## 0.4.1 - 2026-09-21

Typing `tofu` in a project you have never opened walks you through it, the running row says what it is actually doing, and the model can reach the network.

### Added

- **the first run.** With no subscription signed in and no key stored, the app draws what is missing and the command that fixes each, instead of an error. Run `tofu login` in another terminal and it notices within a second and moves on without a restart. The key is never in a frame, a log or a recorded session
- **`fetch` and `web_search`.** A page comes back as units a model can use rather than as markup: 67,692 bytes of one documentation page became 37,274, with every code block intact and links carrying their targets. A page too large becomes a handle. A page that is not text says so. Every fetched page arrives between markers with a per-call random id, as data that cannot issue an instruction. The search provider is catalog data, so changing it is a file rather than a commit, and with no key stored the tool is absent from the list rather than failing when called
- **`tofu why` shows the ten nearest precedents** for a decision, why each one is on the list, and which of them carries a human outcome. A decision now records a fingerprint of the call it judged, so two runs of the same command against different arguments are recognised as the same kind of decision
- confidence is computed here from the vendor's own published formulas rather than read off the response, and both numbers are kept. They agree to within 0.027 across 1,751 recorded answers
- three rules for what a test may not do, in the catalog, so they reach any repository: an assertion that only checks a value exists is not an assertion, a mock stands at a boundary, and a test file covers the empty case and the boundary
- a recorded session carries the ceiling it ran at, the target that came from it, and one sentence saying whether automatic compaction was on and why. `tofu session info --json` reads all three
- `symbols`, which answers which line declares a name and which function each call sits in, from `go/parser` with no cgo. `grep` answers neither
- a bash result carrying a `file:line` citation is checked before the model sees it, and a citation that does not resolve is refused by name rather than warned about. Results this binary generated are not checked, because their citations are true by construction

### Changed

- **the running row has three phases and its clock counts the work.** `requesting` while the provider has said nothing, `thinking`, then `working` with the tool's name. The count starts when the first token arrives, so it no longer includes the wait. A phase holds for 400 ms before another can replace it, which is what stops it flickering between two tool calls, and the whole row is one colour per phase. A turn closes with `finished in 13s`, and the wait only when the wait was longer than the work
- a finished step is drawn where the running row was, because the transcript is bottom aligned now rather than padded downward
- **the ceiling tofu operates under is its own, and it does not move with the model.** A million token window does not mean a million token request is a good idea, it means an expensive one. The model's window is a wall that refuses a request that could not have worked, and it comes from a registry with a shipped offline snapshot rather than being typed into each model's file
- a token estimate now counts the instruction prefix and the tool schemas, which every request pays for and nothing counted. It was up to 84 percent out against what the provider billed and is now 14
- a child that writes outside its paths asks for the path once and waits, instead of the refusal being something a worker can route around
- a tool call repeated inside one turn is answered once. A write to a path clears it, and a cached answer says it was cached
- `read` on a path that does not exist repairs it when exactly one file under the working directory has that name, says it did, and refuses with both named when two do
- a fork carries what was found rather than a list of byte counts. The new session gets one line per source with what came back, instead of a list of calls and their sizes

### Fixed

- **a spawned child's session events were written twice.** The child records itself and the parent wrote the same row again afterwards

## 0.4.0 - 2026-09-20

boji is tofu. The minor number carries it because the name is the command you type and the directory your history lives in, which is exactly what a version promises.

### Changed

- **the binary is `tofu`.** `tofu run`, `tofu session`, `tofu usage`, every verb as it was, with the same flags and the same exit codes. Install it and the old `boji.exe` can go
- **the data directory is `.tofu`**, in the project and at `~/.tofu`. The first run that finds only the old one copies it and says so, with the counts, and the old directory is read and never touched: it is still there and deleting it is yours to do. A second run copies nothing and says nothing. An interrupted copy never becomes the new directory, and the next run throws the partial away and starts over. Your seventy-one recorded sessions and both your logins came across and `diff -r` reports no difference
- the thirty-one `BOJI_*` environment variables are `TOFU_*`. Most gate a test skip, so the skip names were compared one by one before and after, and none appeared or vanished
- the five tools the model can call are `tofu_lint_comments`, `tofu_rules_check`, `tofu_judge`, `tofu_why`, `tofu_replay`. The four recorded sessions that carry the old names still read and still replay

### Not changed

- ticket ids stay `BOJI-NNN`. An id is an identifier and not a brand, and three other tickets reference each one
- `CHANGELOG.md` and the planning documents keep the words they were written with. They are the record of what was decided when it was called that
- `bench/corpus/` keeps one old environment variable name inside a recorded command and one old import path inside a size fixture. Both are recorded evidence with state hashes: changing a byte changes the measurement

## 0.3.7 - 2026-09-20

The last version under the name boji. A model is named by its provider, a role picks the model, and the composer takes what you type while a turn runs.

### Added

- **a model is `provider/name` and nothing else resolves it.** `anthropic/claude-opus-5`, not `claude-opus-5`. The catalog is a directory per provider under `catalog/models/`, with the subscriptions in `catalog/subscriptions/`, and a file missing a required field is refused by its own name and the field it lacks rather than skipped. `boji catalog` counts every kind and prints what each one requires
- **a role binds a model to a job.** `catalog/roles/turn.yaml` and `catalog/roles/child.yaml`, one field, `model`. The turn you drive and the children it spawns can run on different models on different accounts, so a child can work on the Codex subscription while the turn runs on Anthropic. Nothing is bound in the shipped tree, so a plain run resolves exactly as it did before, and `boji models` says which role reaches which model and which role has nothing bound
- **typing while a turn runs queues instead of doing nothing.** Enter puts the text in a queue, clears the composer and shows the row in the transcript marked as waiting. Several queue in order, any of them can be removed before it runs, and the mark clears when the model actually receives it. A message queued mid-turn reaches the model at its next step as a message from you; one queued after the last step starts the next turn. Ctrl+c drops the queue
- **the session record keeps what was read**, every path and its size, findable without reading the whole body, and switchable off. A session can be ended and still resume, and a session past its lifetime is listed as expired rather than deleted. The default lifetime is never
- a created file in the `file edits` tab draws every line it wrote marked as added, and the tally and the content agree: a file reported `+16 -0` draws sixteen added rows. A file too large to draw says how many lines it has instead of drawing nothing. The first row of the sidebar is `feed`

### Changed

- `write` says whether it created the file or replaced it, and a replacement comes back with its diff. A caller can test for a file that changed underneath with `errors.Is` rather than by reading the file again to guess why the write failed
- a child has a state on every exit path, including a cap and a cancel, and a child that returns normally reaches `in_review` rather than `finished`, because finishing is the orchestrator's word. A parked child keeps the work it had already done

## 0.3.6 - 2026-09-20

Orienting in a repository, and the transcript getting its conversation back.

### Added

- `project_report`, one call that answers what a repository is: its size, its languages, its roots, its entry points and its documentation, from one walk that honours `.gitignore`. On this repository it returns in 17 milliseconds. The two `find` commands it replaces, taken from a real recorded run, take 7 to 8 seconds each on a warm cache and one of them spends 7.6 seconds to produce 13 bytes
- a fourth tab, `file edits`, holding every diff a turn made, newest first, with a sidebar of the agents that made them. The transcript keeps one row per edit saying which file changed and by how much. A thirty edit turn costs 69 rows in the conversation where the diffs would have cost 369
- a session has a name you can type. `boji session rename <id> <name>`, and every verb that took an id now takes a name. A session created now is given a readable one
- a bash command has its own deadline, two minutes by default and ten at most, and `timeout_ms` to set it per call. A command that outruns it is stopped and the model is told how long it ran, as a result it can act on rather than a turn-ending error
- every tool says when its answer was degraded, by name: a walk that hit a cap, a file that would not parse, a binary skipped, a cached result gone stale, a command stopped. A degraded result is never rendered as a complete one

### Changed

- the prompt names `find` and says why it is slow here rather than vaguely preferring tools, with the real numbers from the run that prompted it
- a recorded step carries the four band caps it was measured against, so a session read after the caps move is not shown against numbers it never used

## 0.3.5 - 2026-09-20

`boji changelog`, and a running child that says what it costs.

### Added

- `boji changelog` prints what changed since the version you last read, from a copy carried inside the binary, so it works in any directory. Running it again says there is nothing new. `--all` prints every version and `--json` carries every version as data. Neither of those two records that you read anything
- a running child in the activity block carries its elapsed time and the tokens it has spent, live, where both read as nothing until the child returned
- the tools that walk files read `.gitignore` and honour it. In this repository `grep TODO|FIXME|XXX|HACK .` went from 82,669 files and 2.57 GB to 636 files and 3.4 MB, and its result from 15.6 million tokens to 117, because the walk used to read nine cloned repositories on every search. `glob`, `grep` and `search` each take `include_ignored` to turn it off, and say so in the result when it is off. A `CLAUDE.md` or `AGENTS.md` is read whatever an ignore file says, so an agent can always find the project's own rules
- **you can select text in the app while a turn is running.** The app repainted four times a second to move the spinner, and a repaint clears whatever the terminal has selected, so copying out of a running turn was impossible. It now paints only while something is actually moving: a running tool call still animates, a turn waiting on the model does not
- `ctrl+y` copies the last answer, `alt+y` copies the last tool call with its result, and `/copy` does what `ctrl+y` does. A copy that fails says so rather than reporting a success it did not get
- a slash at the start of the composer opens a command menu, which filters as you type. Four commands: `/session`, `/crew`, `/settings` and `/quit`. A slash anywhere else is ordinary text, so `explain /internal/turn`, `// a comment` and `(/ ` all reach the model unchanged, and `\/` escapes one. Previously `/settings` was sent to the model as a task

- **a stopped turn keeps the conversation.** Pressing ctrl+c and then typing "continue" used to reach a model that had never seen anything you said, so it started the task over. Every message a turn produced is now carried whatever ended it: a cancel, a cap, a model error or an answer
- the turn is written to disk while it runs rather than after it ends, so a ctrl+c, a crash or a model that never answers no longer loses the tool calls already made. Measured at 3.8 to 4.9 ms on a turn with twenty tool calls, against real recorded turns of 100 to 233 seconds
- a reply cut off by the provider's output token limit is recorded as `truncated` rather than as a finished answer, and any tool call it was part way through issuing is refused rather than run, because a lenient parser can salvage arguments that look complete and are not. The model is told why and can issue the call again
- `end_reason` in a stored bench row can be `output_truncated`, for the same case

### Changed

- an outcome name is written in one place rather than two, so the name a session records and the name a session reads can no longer drift apart
- the outcome a retired wall clock cap produced is named as retired. A session recorded before the cap was removed still reads
- ctrl+c stops the turn once and says so. It used to cancel silently, so pressing it again wrote the same line again and again with no sign anything had happened. A second press while the turn is unwinding does not quit, and the footer says so
- a turn you stopped reports as stopped. It used to report the provider error the cancel produced, in red, including the vendor URL

## 0.3.4 - 2026-09-20

The gate can refuse, and the instructions are cached from the first request.

### Added

- `boji run --gate off|shadow|enforce` chooses how the tool gate behaves for one run, and `--no-gate` is the same arm as `off`. With no flag the policy's own declared mode decides
- under `enforce` a `deny` refuses the call: the tool does not run, the model is told in the result it reads and why, and the turn continues so it can take another path. A gate that cannot answer refuses as well, because a check that cannot run costs whatever the tool was about to do
- under `enforce` an `ask` with nobody to answer refuses and says nobody was available, which is every `boji run`

### Changed

- `boji usage` draws a bar per quota window, right-aligns the percent, and writes a reset a person can plan around. `resets in 75h26m` is now `resets in 3d 3h`, and under an hour it is minutes. A window the provider did not report is hidden rather than printed as unknown, and only the fullest window is coloured, at a threshold of 50 and 80 percent
- the app's status bar and `boji usage` draw the same quota through the same code, so a window at 72 percent looks identical in both. That cost the status bar its clock: `62% resets 18:00 (in 6h)` now reads `62% resets in 6h 25m`
- a percentage and its bar can no longer disagree. The headline rounded half to even and the meter rounded half up, so the same window could print two different whole numbers
- the first request to Anthropic now caches the whole instruction prefix rather than none of it. On this repository's own prompt the first send writes 5,931 tokens and the second reads all 5,931 back. Before, the first send cached nothing at all: the marker sat on a block too short to meet the vendor's minimum cacheable length, so a session that never got a second turn paid full price for every token of it
- the cost of that is one cache marker. The wire allows four per request and the instruction prefix now takes three, so on a long conversation the older history anchor is dropped and a read after the tail changes falls back to the system prefix alone

### Nothing turned on

Every policy still declares `mode: shadow`, so a deny still records and still runs. `enforce` exists and nothing selects it. The app cannot ask a person yet either: the verdict and its answers are drawn, and no key is bound to them.

## 0.3.3 - 2026-09-20

- the model is told where it is: the directory, the date, the platform and the branch, plus `CLAUDE.md` and `AGENTS.md` walked up from the working directory, nearest winning. It used to run a tool to discover rules written where it was standing
- a tool result is cut at 32 KB rather than 8 KB. Measured against this repository, 400 of its 401 Go files now pass whole, where before a read was a read plus a fetch and each fetch threw the cache away
- a turn has no wall clock limit. It stops when the work stops
- the activity block sits above the composer and carries the running turn and every running child

## 0.3.2 - 2026-09-20

- a second message in a session continues the conversation. Every send used to start again from the task alone, so nothing said in the turn survived to the next one
- one unreadable session record no longer takes down `boji context`. The row is skipped with its reason and the rest list

**The patch number moves on every accepted batch, not only on a big one**, by the owner's instruction on 2026-09-20: "please anything you adding to today that is not upadting to major, please updated the 0.3.x because i feel i'm stuck and you did a lot of changes and yet, i'm not on pair of what is there." So 0.3.1, 0.3.2, 0.3.3, and the minor still carries a break. The bump and the rebuild happen in the same turn, so `boji version` is how he tells what he has.

## 0.3.1 - 2026-09-20

Readable output, a screenshot you can paste, and a frame that holds at any alphabet.

### Added

- a screenshot pastes into the composer with `ctrl+v` or `alt+v`, from a picture on the clipboard or from a copied file, saved beside the session record. Snipping Tool and browsers put a PNG on the clipboard directly, so the common case needs no conversion. A Print Screen bitmap carries a zero alpha channel and is forced opaque, without which the image would be written invisible
- a tool result says whether it failed and how big it was, so a failed call is visible rather than inferred from its text, and the run's one line summary carries a size
- `boji doctor`, `boji usage` and `boji models` each take `--json`, carrying every field the readable form collapsed

### Changed

- `boji doctor` answers on its first line, `ready` or what is missing with the command that fixes it, and is fifteen lines where it was twenty. Six policies with identical thresholds are one line that says six. The repository root is printed once. `boji usage` and `boji models` follow the same shape, and twelve excluded models collapse to five counted lines carrying the catalog's own reasons
- `boji` no longer asks which subscription runs the session. It starts on one and the header names it
- the layout measures terminal cells rather than runes, so a path or a branch name carrying CJK or an emoji no longer overflows the frame. Measured at 113 columns inside an 80 column frame before the fix
- a rate limited quota check says so, where it used to report the quota as unreadable

## 0.3.0 - 2026-09-20

The app, and the gate. Typing `boji` opens something you work in, and the turn still asks a typed question before it acts.

### Added

- `boji` with no arguments is an app. It comes up in the directory you are standing in, takes a task in plain words, works it in a loop and shows every tool call and every result as it happens. Ctrl+C stops the turn and leaves the app up. With a credential missing it names what is missing and offers to run the login rather than printing an error. Every verb behaves exactly as it did, so a script that drives Boji is unaffected
- `boji` starts on the first subscription signed in and asks nothing. The top bar carries the wire and the model it resolved to, `anthropic → claude-opus-5`, so what it chose is visible without anybody being asked, and the quota line follows the subscription in use. An earlier build asked which subscription to use before the session started; that question is gone, and `/model` is where it changes
- the app has a tab strip, `tab` and `shift+tab` to cycle, a digit to jump, `esc` back to the session, and a click on a name. In the session view a digit types a digit, because that view is the composer
- a settings view, listing every provider with the file that decided it: the credential store, `~/.boji/.env`, or the last decision. A stored key is shown as four characters and never in full
- `boji doctor` says which arm spends money and which spends a subscription quota, one line per wire, read from the same place a run reads it so the two cannot disagree
- **breaking:** `boji rules check` no longer prints an override rate and prints `fires: N, blocked: N` instead. The rate could only ever be zero, because nothing in a rule scan asks a person anything, so nothing could record an override. The `--json` report and the `.rules.jsonl` record lose their `overridden` field with it. It comes back when there is a place a person waves a fire through, which is the gate's ask verdict rather than a batch scan
- `boji sift` reads a message on standard input and prints it with what is worth reading kept and the rest elided, and `boji sift --restore` puts it back byte for byte. `--arm` picks how it decides: `signpost`, the default, drops a markdown heading or a short line ending in a colon, free and instant; `brevity` is the word count and banned word list; `jev` asks the typed decision. On a hand labelled set of 57 paragraphs the default agrees 89 percent of the time, Jev 84 to 86 at a third of a cent and a second per message, and the brevity arm 26
- `boji login openrouter` asks for the key without echoing it, proves it reaches Jev with one call, and only then writes it to `~/.boji/.env` at mode 600 where the platform honours it. A key that does not answer leaves nothing behind. The key is never an argument and never reaches a log. A project `.env` still wins over the stored one
- `boji run` has six tools instead of three: `glob`, `grep` and `edit` join `read`, `write` and `bash`. `edit` takes an exact span of text and replaces it
- `boji run` can call Boji. `boji lint comments`, `boji rules check` and `boji judge` are tools a turn may use, so an agent runs this project's own checks instead of imitating them. Recursion is bounded at depth 2
- a child that reports done gets asked whether it is. `boji run --done-review typed` puts the claim through the `stop_check@1` battery and re-opens the child once, in words, with the reason and the decision id, so `boji why <id>` prints the chain behind the re-open. `--done-review cheap` is the arm that decides on whether any tool call ran clean, and `off` is the default and the behaviour until now. A Jev error leaves the claim standing with the reason on the child's row rather than re-opening on a broken backend
- a spawn onto a path another child already holds returns that child, the glob that collided and what it has reported, instead of an error. The roster still refuses; the refusal now carries the one correct move
- a turn can hand work to a child that owns its own paths, refused at the write if it strays, and refused outright if two children overlap. The child's row reaches the ledger as a turn of its own and the parent's cost covers the tree it started. `--no-crew` is the off arm
- a context budget, with the byte-per-token figure measured on this project's own content rather than assumed
- a session that outgrows its budget forks instead of being rewritten in place. The old session ends whole and a new one begins carrying the work forward, so the next request is one fresh prefix and then appends. Rewriting the middle of a live conversation costs 58 percent more than doing nothing at all, because it destroys the cache it was meant to save. The fork carries handles to what it dropped, which is instant; a written summary was measured at 21,931 ms, stale before it arrives, and saves 5 percent. The off arm is in the loop configuration and has no flag yet, unlike `--no-gate` and `--no-crew`
- `boji run` asks the tool gate battery before every tool call and records the answer. The decision row names the turn and the turn row names the decision, so either one leads to the other
- `boji run --no-gate` is the arm that turns the gate off, and `--max-decisions` caps the spend. A turn that reaches the cap ends with its own outcome rather than continuing quietly
- `boji rules list` and `boji rules check [path]` run the rules in the catalog over a tree and print every fire with its mode, whether it blocked, and the override rate. `--catalog <dir>` points at a scratch catalog, so an enforced mode can be tried without touching the one that ships
- a rule may declare an exception in its own file. `em_dash` declares `except: quoted`, so an em dash inside a fence, an indented block, a backtick span or a double-quoted span is the author quoting rather than the author writing
- a turn session file carries a schema version, and a decision row carries the turn it came from

### Changed

- the shell tool runs `sh -c`, not `cmd /C`. On Windows `cmd` has no way to escape a quote inside a quoted argument, so 24 of 32 recorded quoted commands reached the tool as a literal backslash-quote, matched nothing, and returned empty with exit 1, which a model reads as an answer
- the request to a subscription marks the conversation as cacheable, not only the system prompt and the tool list, and the cache lives an hour instead of five minutes. The token count is unchanged; what those tokens cost is not
- the Codex request carries a cache key for the whole session, so its own prefix cache can find the previous step
- `boji doctor` finds a key stored in the home file, from any directory, and still names where it looked when there is none
- `boji run` sets a system prompt naming the tools it has. A tool a model is not told about is not offered, which is why the first run with six tools called neither `glob` nor `grep`
- `boji rules list` and `boji rules check` carry their catalog in the binary, so they work in any tree rather than only in this repository. A project with its own `catalog/rules` still wins, and both verbs name which set decided, as a first line in text and as an `origin` field in `--json`
- **breaking:** `boji rules list --json` emits `{"origin": ..., "rules": [...]}` where it emitted a bare array. A reader that expects an array at the top level needs one field of indirection
- `boji_judge` takes a `state` and a `battery` instead of a raw json body. A model could not guess the old shape: in one recorded run it tried six times, first with a list, then with four different spellings of a question type, and failed every time
- the stop check corpus is nineteen frozen files under `bench/stopcheck/`, not whatever the live ledger happens to hold. Running a turn in this repository no longer changes what a measurement is measured against
- the three copies of the percentile arithmetic under `bench/` are one package, `bench/stat`. The three were identical, so no published figure moved
- the boji arm of `bench harness` no longer measures whichever binary invoked it. It builds `./cmd/boji` and measures that, or measures the one named by `--boji <path>`

### Removed

- `boji bench` is gone, with its five targets. The measurement lives in its own binary, built from `bench/cmd`, so an edit under `bench/` cannot break `boji login`, `boji why` or any other verb. Run `go run ./bench/cmd <target>` from the repository root, or build it once with `go build -o bench ./bench/cmd`. `boji bench api` becomes `bench api`, and `cost`, `wording`, `turn` and `harness` follow the same way. Every flag is unchanged and the reports are byte for byte what they were

### Fixed

- the gate was off in every directory except this repository. The policy was looked for beside the project rather than inside the binary, and the key was read from `./.env` alone. The policy ships in the binary now, a project may override it, and the row says which one decided
- the Codex wire made up a new session identity for every request, so nothing it sent could match anything it had sent before
- a turn session file never recorded its schema version, so every one written before today reads as schema 0 against version 1
- `boji judge` never recorded which state builder made its state, so `boji why` said the writer had not adopted it. Every row from `boji check` had it and no row from `boji judge` did

## 0.2.0 - 2026-09-18

The record. Every decision can be explained and re-scored without asking the model again.

### Added

- `boji why <id>` prints the chain behind a decision: every answer with its distribution, the verdict, the rule that fired, the threshold it compared against, and a line naming any question that sat inside the dead band
- `boji replay --point <name> --set <threshold>=<value>` re-scores every recorded decision against changed thresholds, with no network call in its import graph, and counts how many changed verdicts now disagree with a recorded outcome
- `boji check '<command>'` decides on a real shell command and records the row without blocking anything, and `boji label <id> <outcome>` attaches the answer a person would have given
- `boji lint comments` reports every comment in the tree with its position, using the standard library parser rather than a text match
- a policy: thresholds as catalog data, a closed verdict type, authority nouls that relax and never tighten, and a dead band taken from the measured rerun spread
- a state builder with a version derived from its own shape, recorded on the row

### Changed

- a ledger answer keeps the kind it was: a noul is a number, not a probability parsed out of a field named for a different answer kind. Schema 2, and rows written under schema 1 still read
- the request cap is two documented token limits instead of one invented byte number, with an estimator built from five measured points and deliberately pessimistic where it has no data
- a bare catalog name is refused when more than one version exists, naming every version it found, rather than silently taking the newest

### Fixed

- `boji check` put the command itself into the recent user messages, so a question about what the user asked for answered 0.91 about a request nobody made, relaxing a force-push from ask to allow. It is 0.03 and ask now
- `boji doctor` reported the key as missing while `boji judge` was using it, and now names where the key came from
- a converter existed three times and two copies silently dropped a choice option's criteria

## 0.1.0 - 2026-09-18

The instrument. A typed decision can be asked, validated, recorded and measured.

### Added

- `boji judge` reads a state and a question battery on standard input and prints typed answers, writing a row for every call and answering a repeat from cache with no network
- `boji bench api` measures latency by state size and question count, rerun agreement, the option ceiling and cost per decision, and writes a dated report
- `boji bench cost` compares Jev, two frontier models and a plain regular expression on the same six decisions
- `boji doctor`, `boji catalog resolve`, `boji version`
- the Jev wire with structural validation: probabilities that sum, a chosen option inside its criteria, an argmax that agrees, a score level count that matches
- the decision ledger, append only, one file per day, with the full distribution on every answer, and a replay cache keyed on canonical JSON
- the question catalog with a build-time linter, and a YAML subset parser written by hand rather than taking a dependency
