# Changelog

Kept by hand, in the shape of keepachangelog.com, and versioned by semver.

**`.local/boji/diagram.html` is updated in the same edit as every entry below.** It draws what runs where, keeps a version selector so an older shape can be read back, and marks in green what changed in the selected version. It exists because too much moves in a night to hold in one head.

Tofu is personal and not released, so the public interface that a version promises is the command line and the file formats: the verbs and their flags, the exit codes, the catalog schema, the ledger row schema and the policy schema. A version says what changed for someone driving the binary or reading its files, not what changed inside it.

The minor number carries breaking changes, which is what 0.x means, and **there is never a 1.0.0**. The owner decided that on 2026-09-19: this stays 0.x forever, so the promise the version makes is the one 0.x already makes, that anything can break on a minor bump.

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
