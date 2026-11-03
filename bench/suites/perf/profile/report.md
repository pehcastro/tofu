# bench profile: what tofu's memory and CPU are made of

**tofu itself is 43.5 MB of the 387 MB tree peak on the bench/rivals task (11%) and 2.38 s of its 55.1 s CPU (4.3%).** The rest is the model's own commands: `go vet` compiling the standard library cold is 318 MB of the peak and 30 s of the CPU, and `go test` is 21 s. Inside tofu the Go heap in use never passes 17 MB; its working set is half private (22.6 MB) and half shared image pages (21.7 MB).

**One sub-agent adds 1.3 MB of tree peak and 0.117 s of CPU.** tofu's own process is 1.2 MB and 0.103 s of that, the undo snapshot's `git` 0.017 s. Half of what each sub-agent allocates is the project registry read from disk and decoded again.

Runs on 2026-10-13, 18:06 to 18:17, windows/amd64 with 32 logical CPUs, one at a time behind the shared rivalslot. Live rows on the owner's claude-sub subscription (claude-sonnet-5-5), cassette rows on a fresh home. Build 0.5.13-fix1+dev at f360965a, go1.27.1. `PROFILE_WORK=<scratch> PROFILE_SLOT=<slot dir> bash bench/profile/run.sh`, or `PROFILE_LIVE=no` for the cassette rows only; `go run ./bench/profile report` prints every row from `runs/`.

## How it is measured

- A Windows job object holds the whole tree and posts each new member to a completion port; every member is opened at once and kept, so its final CPU and peak working set are read even after it exits. 421 of 421 processes seen on the rivals task, CPU per process summing to the job's.
- Tree RSS is the live members' working sets summed every 100 ms (bench/rivals and bench/scale sample every 250 ms).
- A process's kind comes from its ancestry: under `go.exe` it is `go <verb>` of the outermost one, under `gofmt.exe` it is gofmt, under a shell it is shells, tofu's direct children keep their image name.
- tofu profiles itself when `TOFU_PPROF=<dir>` is set: `cpu.pprof`, `heap-peak.pprof` each time the heap in use grows by 1 MB, `heap-end.pprof` and `memstats.json`, written before `os.Exit`. `profile sample -pprof` sets it; the stock rows run the same binary without it. The rows above the TOFU-1319 section were taken with the `go build -overlay` hook that came before it. Profiling costs 8.6 MB and 0.09 s; every size below comes from the stock rows, profiles only attribute.
- Go's CPU profiler on Windows counts threads blocked in a system call; every CPU top drops waiting samples, and what is left is within 10% of the process's own CPU time.

## The bench/rivals tofu-sonnet run, split

73.4 s wall, job CPU 55.14 s, peak tree RSS 387.0 MB, peak commit 533.4 MB.

| kind | processes | MB at the tree peak | largest one process MB | CPU s |
|---|---|---|---|---|
| go vet | 209 | 318.0 | 263.7 | 29.95 |
| go test | 167 | 0.0 | 144.7 | 21.30 |
| tofu | 1 | 43.5 | 44.3 | 2.38 |
| shells | 21 | 25.5 | 11.4 | 0.50 |
| git (undo snapshot) | 18 | 0.0 | 8.2 | 0.72 |
| go version (tofu's toolchain probe) | 2 | 0.0 | 13.2 | 0.19 |
| rtk (the owner's hook) | 1 | 0.0 | 19.0 | 0.06 |
| gofmt | 2 | 0.0 | 8.1 | 0.05 |

The spread between runs (376 to 412 MB) is in `go vet`, not in tofu (42.1 to 43.5 MB).

**tofu's heap**: in use peaks at 17.0 MB; 164.1 MB allocated over the run in 32 collections, at most 37 goroutines. `sys.readProjectRegistry` is 61.2 MB of the 144.6 MB allocated (41%): every ledger row (48.7 MB), the shell sift's scorer (29.2 MB) and the gate (23.2 MB) read and decode the registry again.

**tofu's CPU** is system calls, not Go code: stat and open 0.46 s, a thread walk 0.19 s (`shell.resumeSuspended` walking every thread on the machine), process creation 0.11 s. By function: `turn.gatedCall.run` 0.65 s, `main.appendRow` 0.48 s, `main.(*toolGate).Decide` 0.34 s, `boundClassifier` reopening the credential store 0.16 s. Garbage collection 0.04 s.

## One sub-agent added, on the bench/scale tiny task

| agents | wall s | job CPU s | peak tree MB | tofu own peak MB | tofu CPU s | git processes |
|---|---|---|---|---|---|---|
| 0 | 3.9 | 1.00 | 41.8 | 37.7 | 0.47 | 12 |
| 1 | 8.7 | 1.31 | 44.5 | 38.7 | 0.67 | 16 |
| 10 | 30.4 | 2.17 | 54.9 | 49.5 | 1.50 | 23 |

Per sub-agent: 1.31 MB of tree peak (bench/scale fitted 0.4 MB over seven rungs; one pair here, so neither is settled), 0.56 MB of heap, 4.7 goroutines, 0.117 s CPU, 8.4 MB allocated of which 45% is the registry. `SpawnTool.background` is 55 ms per agent: 17 ms re-reading and saving the session header, 13 ms re-reading settings for the spawn's limits, 12 ms reopening the credential store. On a fresh home `gh api user` (39 MB, 0.2 s), the once-a-day memory identity probe, is the tree peak.

## Where tofu can get smaller and faster, ranked by the saving measured

1. **Undo snapshot without four `git` processes a step** (`internal/snapshot`): up to 0.7 s CPU a task, 47% of the tiny task's tree CPU, 8 MB at a coincident peak.
2. **The `gh` identity probe off the startup path** (`internal/memory/author.go`): 38.6 MB at the peak and up to 0.5 s of startup, once a day per home.
3. **Resume a suspended shell with NtResumeProcess** instead of walking every thread (`shell.resumeSuspended`): 0.21 s CPU a task, about 50 ms per bash command.
4. **Gate credentials and the ledger's directory resolved once per process**: about 0.3 s CPU a task, 12 ms per sub-agent.
5. **A sub-agent's settings and session header kept in memory**: about 30 ms of the 103 ms a sub-agent costs tofu.
6. **The project registry cached per process, keyed by modification time**: 41% of all allocation, 2.1 MB per sub-agent; small in time (about 20 ms).
7. **The Go toolchain probed once per process** (`internal/turn/tool_bash.go`): 0.1 s CPU and one 13 MB process a task.
8. **Git for Windows' real bash, not its launcher**: one 6.1 MB process and about 20 ms less per command, Windows only.
9. **The HTML sanitizer's init deferred**: about 5 ms and 0.75 MB per process start.

Not ranked: 21.7 MB of tofu's working set is shared pages from the 49.7 MB binary and system DLLs; `tofu version` alone peaks at 18.7 MB.

## After TOFU-1319

Items 1, 3 and 8 are in, item 6 in part, and `TOFU_PPROF` replaced the overlay. **tofu's own CPU on the rivals task went from 2.38 and 2.14 s to 1.28 and 1.14 s, its allocation from 144.6 and 176.3 MB to 66.9 MB, and its git processes from 18 to 7.** Runs on 2026-10-13, 19:00 to 19:35, same machine, same slot, before and after binaries built from one tree; `runs/` holds the final after rows (no suffix), the rows of a first after build that cached the registry in a package variable (`after-…-r2` to `-r4`, withdrawn because this project holds no global state), and the second before round (`before-…-r2`). The first before round is the table above.

- The undo snapshot writes its tree in process from the undo index (format 2 and 3; anything else, a split or sparse index, or a sha256 store falls back to `git write-tree`), so a step is `ls-files` and `add`, two processes instead of three. The tree ids match git's: a driven turn recorded the seed commit's tree as its start and the same id `git write-tree` prints over the undo index as its end.
- Prune runs at most once an hour per store, and never on a store younger than an hour, because nothing in it can be older than the one hour grace: the stamp is `undo.git/objects/info/pruned`. Before, every session start ran one `write-tree` per session folder, then `update-ref` and `prune`.
- A suspended shell resumes with one `NtResumeProcess` call instead of a snapshot of every thread on the machine.
- Finding a registered project streams the registry and decodes only each entry's folder and paths: 194 KB a lookup instead of 957 KB over a 1100 entry registry, about the same CPU (3.3 against 2.9 ms). An unregistered project, a registry edit and `ProjectGlob` still decode it whole. Decoding it once per process needs a value the callers share, which they do not have yet.
- Git for Windows' `bin\bash.exe` launcher is replaced by `usr\bin\bash.exe` with the environment the launcher sets (`MSYSTEM=MINGW64`, `EXEPATH`, `PLINK_PROTOCOL` when unset, `HOME` when unset, and `mingw64\bin`, `usr\bin` and `$HOME\bin` before `PATH`), checked by diffing `env` under both: one process fewer per command. Only when that layout exists; every other shell, and every other OS, runs as before.
- The key redactor skips its regular expression when no key name occurs in the text.

| row | tofu CPU s | job CPU s | git processes | tool calls | allocated MB |
|---|---|---|---|---|---|
| rivals before, TOFU-1317 | 2.38 | 55.14 | 18 | 25 | 144.6 (profiled row, 16 calls) |
| rivals before, r2 | 2.14 | 52.16 | 18 | 14 | 176.3 (profiled row, 35 calls) |
| rivals after | 1.28 | 50.16 | 7 | 19 | |
| rivals after, profiled row | 1.14 | 48.41 | 7 | 24 | 66.9 |

Per sub-agent, job CPU from the live tiny task, (10 agents less 0) / 10: before 0.117 and 0.142 s; after 0.104 s, and 0.061, 0.103, 0.070 and 0.098 s with the withdrawn cache. On the cassette, where the turns are fixed, 0.091 and 0.100 s before against 0.098 s after (0.076 and 0.071 s with the cache). What is left per sub-agent is outside these files: the credential store opened by `LoadKeyRedactor` in `turn.Run` (8 ms) and by `cred.Open` in the sub-agent opener (7 ms), and settings re-read through `settingsPaths` (about 9 ms), which are items 4 and 5.

Not done here: item 2 needs `internal/memory/store.go`, where `Block` asks for the author whether or not a user-local shelf exists; item 7 is already once per process, and the two `go version` processes are the go command and the toolchain it switches to.

## What these numbers cannot say

- One run per row; the stock rivals peak moved 25 MB between two runs on the same binary, all in `go vet`.
- Live rows ran on the owner's real home for the login, so his hooks (rtk) and his project registry applied; the registry's size sets item 6's cost.
- Linux and macOS are not measured: the tree split uses Windows job objects, and `profile sample` refuses elsewhere.
- A live row moves with what the model chose to do: the before rivals rows made 25 and 14 tool calls and the after rows 19 and 22, and a 10 sub-agent lead takes two or three turns, each of which snapshots twice.

## Binary size and idle memory, TOFU-1349

**tldr: the SQLite driver is 3.55 MB of the 37.09 MB binary (9.6%) and about 10 MB of the interface's 43 MB idle working set; `go/types`, pulled in for one `ExprString`, is 1.14 MB (3.1%); the terminal library's Windows input reader allocates 8 MB a second at idle, and reusing its buffer takes 4.0 MB (9%) off the idle interface.** Removing the first two sums to 32.4 MB, 12.6% smaller; the two were built apart, not together.

`PROFILE_WORK=<scratch> PROFILE_BUILD_SLOT=<slot dir> bash bench/profile/size.sh`: `git archive HEAD` into the scratch, a stripped (`-trimpath -ldflags=-s -w`) and a symbol build, then `profile size` (top packages by `go tool nm -size`, linker tables excluded) and `profile idle` five fresh starts each. A start runs in a 120x40 pseudo console in a fresh `HOME`, `USERPROFILE`, `APPDATA` and `LOCALAPPDATA` and an empty git project, is read 20 times over 2 s after a 5 s settle, the median kept, and is killed; nothing is discarded or warmed. On a fresh home the interface shows the two-step set-up screen (no subscription, no Jev key), the `screen-*.ansi` files in the work directory hold what it drew. No model is called. A fresh home reloads the models.dev catalog (5.6 MB of JSON) at start, which a real home does once a day; the `noreload` rows point `TOFU_MODELS_REGISTRY_URL` at a refused port, which is what the other 23 hours look like. Windows only.

### HEAD, five fresh starts each

Source: 2292c768, 2026-10-14 09:06, windows/amd64, 32 logical CPUs, go1.27.1. Stripped binary 37.09 MB (38,896,128 bytes); with symbols 50.32 MB.

| reading | min | median | max |
|---|---|---|---|
| interface, working set MB | 44.6 | 45.0 | 54.6 |
| interface, private working set MB | 25.2 | 25.5 | 25.8 |
| interface, peak working set MB | 63.4 | 64.4 | 74.1 |
| interface, catalog reload refused, working set MB | 43.2 | 43.3 | 43.8 |
| interface, catalog reload refused, private MB | 23.8 | 24.0 | 24.5 |
| interface, idle CPU % of one core | 2.3 | 3.9 | 4.7 |
| `tofu serve --stdio`, working set MB | 17.7 | 18.9 | 19.4 |
| `tofu serve --stdio`, private working set MB | 6.4 | 7.5 | 7.9 |
| `tofu serve --stdio`, idle CPU % of one core | 0.0 | 0.0 | 0.0 |

### Top 15 packages by symbol bytes

Source: 2292c768, `go tool nm -size` on the symbol build. 18.96 MB of symbols in 462 packages; the rest of the file is the pc-line table (8.5 MB), strings and type data, which the linker does not attribute to a package.

| rank | package | KB | of the stripped file | pulled in by |
|---|---|---|---|---|
| 1 | modernc.org/sqlite/lib | 1988 | 5.2% | `internal/sys/credential.go:15`, the credential store `agent.db` |
| 2 | main | 1236 | 3.3% | `cmd/tofu` |
| 3 | github.com/yuin/goldmark/util | 767 | 2.0% | glamour, `interface/tui/markdown` |
| 4 | slices | 767 | 2.0% | generic instances across the tree |
| 5 | tofu/internal/turn | 712 | 1.9% | |
| 6 | runtime | 665 | 1.8% | |
| 7 | go/types | 522 | 1.4% | `internal/search/symbols.go:89`, `gounit.go:82`, one `types.ExprString` each |
| 8 | tofu/internal/host | 521 | 1.4% | |
| 9 | tofu/internal/turn/tools | 359 | 0.9% | |
| 10 | crypto/tls | 358 | 0.9% | |
| 11 | github.com/alecthomas/chroma/v2/lexers | 339 | 0.9% | `interface/tui/edits`, glamour |
| 12 | net/http | 327 | 0.9% | |
| 13 | github.com/clipperhouse/uax29/v2/graphemes | 311 | 0.8% | the charm width tables |
| 14 | tofu/internal/boardy | 255 | 0.7% | |
| 15 | net | 237 | 0.6% | |

By family: modernc.org 2145 KB, goldmark, chroma, glamour and regexp2 together 1966 KB, crypto 1278 KB, go/ 725 KB, tofu's own packages 4963 KB.

### Each cause, its fix, and the saving measured in a scratch clone

Source: arms built from `git archive 75d2b58b` (37.09 MB, 38,890,496 bytes), 2026-10-14 08:36 to 08:50, five fresh starts each; HEAD rows from the same base: interface 45.1 MB, reload refused 43.6 MB, serve 18.7 MB.

| cause | where | fix | stripped binary | interface idle, reload refused | serve idle |
|---|---|---|---|---|---|
| SQLite driver for the credential store | `internal/sys/credential.go:15`, used by `internal/llm/cred/store.go` (credentials, refresh leases, use) and `api_keys` | a file store, owner's call | -3.55 MB (-9.6%) | -10.1 MB (33.5), upper bound | -0.2 MB (18.5) |
| `go/types` for receiver names | `internal/search/symbols.go:89`, `gounit.go:82` | a 20 line `receiverType` over `go/ast` | -1.14 MB (-3.1%) | -0.5 MB (43.1) | -0.9 MB (17.8) |
| ultraviolet's Windows reader allocates `4096` input records every 10 ms | `ultraviolet@v0.0.0-20260811164956-006e29f97886/terminal_reader_windows.go:289`, `peekNConsoleInputs` | reuse one buffer across polls | 0 | -4.0 MB (39.6) | 0 |
| `go/types` and the buffer together | | | -1.14 MB | -4.5 MB (39.1, -10.3%) | |

| refuted or not a cause | measured |
|---|---|
| `-buildmode=exe` instead of the default PIE, so relocated pages stay shared | 37.09 to 36.83 MB, interface 43.2, serve 18.8: no change |
| go-runewidth's 2.2 MB `strictWidthLUT` | BSS, built lazily, not in the file or the idle set |
| package init | 1.6 MB allocated and 14 ms across every package (`GODEBUG=inittrace=1`, `tofu version`) |

### The catalog reload and the reader interact

The fresh home's reload of the models.dev catalog (`main.refreshRegistry`, `io.ReadAll` in `internal/transport/client.go:164`) puts the interface's peak at 64.4 MB against 43.4 MB without it. At HEAD the reader's 8 MB a second of garbage forces a collection about every second, which happens to return that memory by the 5 s read. With the buffer reused the collections stop and the reload's memory stays: 63.7 MB at 5 s. `debug.FreeOSMemory()` after the reload in `cmd/tofu/app.go` `modelsReload` brings it to 41.1 MB (-4.0 MB, -8.9% against HEAD's 45.1). So the reader fix ships with the reload's memory returned or streamed, never alone.

| interface arm, fresh home with reload | working set median MB | private MB | peak MB |
|---|---|---|---|
| HEAD 75d2b58b | 45.1 | 25.6 | 65.6 |
| `go/types` gone, buffer reused | 63.7 | 44.6 | 63.8 |
| and `debug.FreeOSMemory()` after the reload | 41.1 | 22.0 | 63.3 |

### What round 2 can reach

- **Binary 10%**: needs the SQLite store gone (9.6%) and `go/types` (3.1%), 12.6% together. Without SQLite the binary target is not reachable from these causes.
- **Idle working set 10%**: `go/types` gone and the buffer reused, on a home whose catalog is fresh: 43.6 to 39.1 MB, -10.3%. On a fresh home it is -8.9% with the reload's memory returned.
- `receiverType` agrees with `types.ExprString` on 34,586 of 34,588 receivers across the five roots and the Go 1.27.1 standard library; the two that differ are receivers that do not compile (`[]int`), in `go/types` test data.

### What these numbers cannot say

- The SQLite arm cannot open `agent.db`, so its idle row skips every read of the store; the 10.1 MB is an upper bound and a file store's own cost is not measured. Its first start read 89.3 MB, unexplained.
- The interface rows are the set-up screen of a fresh home, not a chat with a signed-in model.
- Five starts per row; one start in five lands mid-collection and reads 9 to 15 MB high, which is why the max column is wide and the median is the number compared.
- The ultraviolet fix is a change to a dependency: a `replace` to a patched copy or an upstream change. charmbracelet/crush in `.local/sources` pins the same ultraviolet version and the same SQLite driver, so it carries both costs.
- The idle CPU column is from a 2 s window; the reader's 10 ms poll costs 1 to 4% of a core whether or not the buffer is reused.

## Idle CPU and its wakeups, TOFU-1366

**tldr: idle, the interface takes 3.2% of one core on the set-up screen and 6.4% on a signed-in chat, `tofu serve --stdio` 1.2%; the process-time figures above (0.8 to 4.7%, serve 0.0%) undercount, because Windows charges thread time at the clock tick and these wakeups are shorter than one. Four changes measured together take the chat to 2.2% (-66%), the set-up screen to 0.9% (-73%) and serve to 0.3% (-71%), with typing, resize, the streamed reply and the idle screen unchanged.**

`PROFILE_WORK=<scratch>`, then `profile idle -label <l> -build <b> -work <scratch> -window 10s -home stub|login -- <tofu> [serve --stdio]`, and `profile idles <labels>` for the table. `-home stub` points every URL tofu calls at a local stub, so a fresh home shows the set-up screen with no network; `-home login` adds a mock claude-sub login against that stub and a mock Jev key, so the interface opens a signed-in chat with the welcome art (a fresh session, before its first message). Five fresh starts per row, read for 10 s from 5 s after start, nothing discarded. CPU by cycles is the process's cycle count over the window (one `NtQuerySystemInformation` snapshot at each end) divided by the 3187 MHz nominal clock; context switches are the sum over its threads from the same snapshots. Windows only.

### HEAD, five fresh starts each

Source: 50aae51d, `git archive HEAD`, stripped build, 2026-10-14 14:12 to 14:17, windows/amd64, i9-14900K, 32 logical CPUs, go1.27.1, mock claude-sub login against a local stub, no model called.

| screen | CPU % of one core by cycles | by process times | context switches per s | working set MB |
|---|---|---|---|---|
| set-up screen | 3.20 (3.02 to 3.28) | 1.09 (0.31 to 1.86) | 804 (793 to 815) | 35.7 |
| signed-in chat | 6.45 (6.36 to 6.54) | 4.82 (3.42 to 6.21) | 894 (881 to 909) | 43.7 |
| `tofu serve --stdio` | 1.17 (1.08 to 1.22) | 0.00 (0.00 to 4.66) | 65 (62 to 66) | 22.0 |

### Every idle wakeup, where it is and what it is for

Source: a goroutine dump and a 20 s CPU profile of each idle process, taken in a scratch build of 50aae51d; rates from the code.

| source | where | what it is for | wakeups per s | runs on |
|---|---|---|---|---|
| input reader poll | `third_party/ultraviolet/terminal_reader_windows.go:47`, `time.Sleep(10 * time.Millisecond)` between two empty `PeekConsoleInput` calls | waiting for a key, mouse, resize or focus record | 100 | every interface screen |
| renderer ticker | `charm.land/bubbletea/v2@v2.0.9/tea.go:1405`, flush at `:1425` | writing the newest view at up to 60 frames a second; an unchanged view returns after a string compare | 60 | every interface screen |
| welcome wordmark pulse | `interface/tui/welcome/identity.go:73`, 120 ms at `:25` | the mint dot circling the wordmark; each tick is a full `App.Update` and `App.View` | 8.3 | chat before its first message, **and hidden behind the set-up screen** |
| welcome cat tick | `interface/tui/cat/cat.go:112`, 4 a second at `identity.go:24` | the cat's tail; a full update and view each | 4 | same |
| set-up recheck | `interface/tui/app.go:466`, 1 s at `:192`, calling `cmd/tofu/app.go:136` `wiring.blockers` | noticing a login or key added from outside | 1 | set-up screen |
| credential sweep | `internal/llm/cred/keep.go:28`, 60 s | refreshing tokens near expiry | 0.02 | interface and serve |
| serve shell watch | `internal/host/serve.go:719`, 250 ms; `serve.go:737` calls `sys.LoadKeyRedactor` every tick, which opens the SQLite credential store and compiles a regular expression | streaming shell output to the client | 4 | serve |
| serve settings watch | `internal/host/settings.go:97`, 250 ms | noticing a settings file edited outside | 4 | serve |
| serve board watch | `internal/host/boardy.go:90`, 250 ms | noticing a board file edited outside | 4 | serve |

The intro starts in `interface/tui/welcome.go:29` from `App.Init` (`interface/tui/app.go:435`) whenever the session is fresh, set-up or not: the set-up screen's dump holds three timers, the recheck and both welcome ticks. Each Go wakeup costs about four context switches here (the reader arm removes 100 wakeups and 426 switches a second), because the runtime wakes a spinning thread (`runtime.wakep`, `startm`, `stealWork` across 32 Ps): a profile of the set-up screen puts 26% of its samples in `stealWork` and 22% in `semawakeup`.

### Each fix, measured in a scratch clone

Source: 50aae51d plus one change per arm, `git archive HEAD` into the scratch, stripped builds, 2026-10-14 14:12 to 14:34, five fresh starts each, same machine and conditions as HEAD.

| arm | change | set-up screen CPU % | chat CPU % | serve CPU % | context switches per s, set-up / chat |
|---|---|---|---|---|---|
| HEAD | | 3.20 | 6.45 | 1.17 | 804 / 894 |
| reader waits | the reader blocks in `WaitForMultipleObjects` on the console handle and a cancel event set by `Cancel` and by the context, instead of sleeping 10 ms | 2.10 (-34%) | 5.28 (-18%) | | 378 / 438 |
| renderer on demand | the event loop wakes the renderer after each message, the renderer flushes at most once a frame and sleeps when nothing changed | 2.97 (-7%) | 5.62 (-13%) | | 718 / 806 |
| welcome ticks merged | one 120 ms tick moves the dot and every second one moves the cat (240 ms a frame, was 250) | 2.44 (-24%) | 3.60 (-44%) | | 782 / 813 |
| serve redactor lazy | `scanShells` loads the redactor only when a shell other than a one-shot is listed | | | 0.34 (-71%) | serve 65 to 39 |
| **all four** | | **0.85 (-73%)** | **2.21 (-66%)** | **0.34 (-71%)** | **109 / 161** |

| arm that turns a mechanism off, not a fix | set-up screen CPU % | chat CPU % | working set MB, chat |
|---|---|---|---|
| no welcome animation at all | 2.08 | 1.85 | 33.2 (HEAD 43.7) |
| HEAD with `GOMAXPROCS=4` | 2.22 | 4.07 | 41.0 |
| all four with `GOMAXPROCS=4` | 0.73 | 2.10 | 41.0 |

The animation is 4.6 points of the chat's 6.45 and 10 MB of its working set; on the set-up screen it is 1.1 points that draw nothing, so starting it only when the set-up screen is gone is a saving with no visible change. `GOMAXPROCS` only matters while the wakeups are there.

### The combined arm, driven

- `tofu drive` (fresh home, 120x40, `--fresh --plain`, a one-reply cassette; type, resize to 100x30, enter, wait for the reply): HEAD and the combined arm print the same 202 lines but for the random session, message and request ids and the spinner glyph, which differs between two HEAD runs too. The drive calls `App.Update` and `App.View` directly, so it does not exercise the reader or the renderer.
- A scratch console check runs the real binary in a 120x40 pseudo console on a mock login whose stub streams a 40-delta reply over 4 s: 20 typed words, a resize to 100x30, the reply, then 3 s idle; two runs per build. Typed echo median 35.6 and 25.1 ms on HEAD, 24.0 and 24.7 ms on the combined arm, worst 45.0 against 38.7. Screens after typing, after the resize, after the reply and 3 s later match HEAD once the clock, the ids and the spinner are masked; the only other difference is the dot's place on the wordmark, which also differs between two HEAD runs. 51 and 52 distinct screens in the first 3 s of the stream on HEAD, 57 and 59 on the combined arm.

### What round 2 needs

- The reader: `third_party/ultraviolet/terminal_reader_windows.go` and `cancelreader_windows.go`.
- The renderer: a fork of `charm.land/bubbletea/v2` v2.0.9 under `third_party/bubbletea`, changing only `tea.go` (`startRenderer` and `eventLoop`), with a `replace` in `go.mod`, in the shape TOFU-1352 gave ultraviolet.
- The welcome ticks: `interface/tui/welcome/identity.go` and `interface/tui/cat/cat.go`; not animating behind set-up: `interface/tui/welcome.go` and `interface/tui/app.go`.
- Serve: `internal/host/serve.go` (`scanShells`).

### What these numbers cannot say

- Linux and macOS are not measured. The reader and the 10 ms poll are Windows only; the renderer ticker and the welcome ticks are on every OS.
- Merging the welcome ticks makes the cat 4% faster (240 against 250 ms a frame); no drive or screen read can tell, but it is a change to an animation, and the larger saving, not animating behind set-up, is not measured on its own: the no-animation arm stands in for it on the set-up screen.
- The chat rows are the welcome screen of a fresh session. After the first message the welcome ticks stop, and HEAD's header clock does not move at idle either (3 s apart, the screen is unchanged on both builds).
- The serve rows have no shell running; with one, the redactor still loads every 250 ms.
- The console check is a scratch program that is not in the tree; the screen reader it uses is `bench/startup/frame`'s, which lives in a `main` package and cannot be imported.
