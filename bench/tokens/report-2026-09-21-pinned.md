# where the result bytes go, and what the glob cap saves, replayed against a pinned commit

generated 2026-09-21 20:45 on DESKTOP-AHUN9RO, go1.27.1 windows/amd64, no model call and no Jev call of any kind, no credential of any kind read, corpus `.tofu/sessions` read through `bench/corpus`, replay tree `git archive e3c805d2b86b5e289305f21a88bcde837b37c838`, cap `internal/konst.GlobPathsResultCap = 300`, tool `bench/tokens`, run by `go test ./bench/tokens/ -run TestByteTotalsOverTheRealCorpus -count=1 -v`

Cost unit: unpriced. A money figure is dollars that left the account behind the credential named above, read from the response of the call it names and never from a rate card. This run made no call at all.

Nothing was warmed up and nothing was discarded. Both runs pasted below are first runs of a fresh process.

This file supersedes `report-2026-09-21.md`, which replayed against the working tree. Every figure that file carried appears below in a `live tree` column rather than being lost.

## The shape taken, in one line

The pinned shape: the working tree changed three figures inside a single report and one of them, `*`, changed inside four minutes, so a number that moves while nobody touches the bench is not a measurement and no test can hold it still.

## The commit, and how it was chosen

`e3c805d2b86b5e289305f21a88bcde837b37c838`, `feat: 0.4.6, the gate refuses a bad rule and a failed request is retried`, committed 2026-09-21T20:26:06-03:00.

The rule: the newest commit on `develop` when the pin was taken, which is also the first commit that contains the day's work in progress while every recorded call ran. The corpus is recorded between 2026-09-18 and 2026-09-21 06:09, and the whole of 2026-09-21 was committed at the end of that day, in `c2afd5f` at 20:07 and `e3c805d` at 20:26. There is no commit at all between `d3baeca` on 2026-09-20 18:05 and `c2afd5f`, so the only two candidates are a tree 12 hours older than the last recorded call and a tree 14 hours newer. The older one is short 357 files that the later calls could see, and the newer one is short nothing.

The pin is a constant in `bench/tokens/pinned.go`, not a lookup of `HEAD`, because a lookup would move the next time anyone commits and the whole point is that it must not.

`git archive` holds only tracked files, so the replay tree holds 1,223 files and no untracked or ignored path. The consequence is stated below under what the pin costs.

## Skips, counted and named

Three skip classes. Two occurred.

**Corpus skips: 1 of 110 entries.** `HEAD` is a pointer file, not a recorded turn. Unchanged from the previous report.

**Glob calls that cannot replay: 6 of 142.** This is the figure the ticket asks to be first class, and `TestTheCallsThatCannotReplayAreTheCountTheReportStates` fails if it is not exactly 6, in either direction. Five name `internal/judge/policy` or `catalog/policy`, deleted by `faf598d`, which is an ancestor of the pin. The sixth names `.local`, which git does not track.

```
glob calls that cannot replay: 6 of 142
  cannot replay: turn-18d732e1a8382e74 step 4 {"include_ignored":false,"path":"internal/judge/policy","pattern":"*_test.go"}: glob: internal/judge/policy is not a path under the working directory, and nothing there is named policy: nothing was run
  cannot replay: turn-18d732f3ab8b7b28 step 4 {"include_ignored":false,"path":"internal/judge/policy","pattern":"*_test.go"}: glob: internal/judge/policy is not a path under the working directory, and nothing there is named policy: nothing was run
  cannot replay: turn-18d733d413c55f34 step 6 {"include_ignored":false,"path":"catalog/policy","pattern":"*.yaml"}: glob: catalog/policy is not a path under the working directory, and nothing there is named policy: nothing was run
  cannot replay: turn-18d733f0d52edb34 step 9 {"include_ignored":false,"path":"internal/judge/policy","pattern":"*.go"}: glob: internal/judge/policy is not a path under the working directory, and nothing there is named policy: nothing was run
  cannot replay: turn-18d73a4d4a8f4564 step 3 {"include_ignored":false,"path":"internal/judge/policy","pattern":"*"}: glob: internal/judge/policy is not a path under the working directory, and nothing there is named policy: nothing was run
  cannot replay: turn-18d742bd38c430c4 step 3 {"pattern":"*.md","path":".local","include_ignored":true}: glob: .local is not a path under the working directory, and nothing there is named .local: nothing was run
```

**Pinned tree unavailable: 0 occurrences.** The bench needs `git` on the path and the pinned commit in the checkout. It needs no network and no credential. When either is missing, every test in the package skips with `ErrNoPinnedTree` naming the commit, rather than falling back to the working tree. On this machine, git 2.53.0.windows.2, it did not fire.

## What the pin costs

Six calls name a path that git does not track, so their path counts are gone rather than merely different. Five of them now match nothing and one is the replay failure above.

| turn and step | args | live tree | pinned |
|---|---|---|---|
| turn-18d742bd38c430c4 step 3 | `*.md` under `.local`, ignored included | 15,016 | cannot replay |
| turn-18d724d865ed9264 step 13 | `.local/boji/tickets/**/*.md`, ignored included | 348 | 0 |
| turn-18d6f8d9e45f8efc step 8 | `.local/**/*.md`, ignored included | 45 | 0 |
| turn-18d6f0fb4994d0ac step 5 | `.local/boji/planning/*.md`, ignored included | 2 | 0 |
| turn-18d6f22be5cb6180 step 3 | `.local/boji/planning/*.md`, ignored included | 2 | 0 |
| turn-18d6f49819dff224 step 9 | `.local/boji/planning/*.md`, ignored included | 2 | 0 |
| turn-18d73582f35a0ac8 step 4 | `CLAUDE.md` | 1 | 0 |

`CLAUDE.md` is the seventh and it is a different reason: `.gitignore` line 13 excludes it, so it is not in any commit. The glob tool always lists a project instruction file even when an ignore rule excludes it, which is why the live tree saw it at all.

This is the price of the shape and it is paid in the open: two of the three calls that a cap of 300 was supposed to protect the model from were sweeps over `.local`, and neither is measurable from a commit. What remains measurable is every call over tracked source, which is 136 of 142.

## What moved, live tree against pinned

Corpus and call counts:

| figure | live tree | pinned |
|---|---|---|
| turns read | 109 | 109 |
| corpus skips | 1 | 1 |
| glob calls counted | 142 | 142 |
| glob calls that cannot replay | 5 | 6 |
| calls matching nothing | 48 | 54 |
| calls matching 1 to the largest scoped call | 76 | 71 |
| calls matching more than 300 | 13 | 11 |

Bytes. Every before-the-cap figure is read from the session files and none of them moved, because the pin changes the replay and not the recording:

| figure | live tree | pinned |
|---|---|---|
| total bytes before | 54,295,422 | 54,295,422 |
| glob bytes before | 48,186,030 | 48,186,030 |
| glob share before | 88.7% | 88.7% |
| glob bytes after | 203,853 | 193,346 |
| glob share after | 3.2% | 3.1% |
| total bytes after | 6,313,245 | 6,302,738 |
| saved | 88.4% | 88.4% |

The 10,507 bytes of difference in the after column are the six calls above losing their matches. The headline is unchanged: `glob` is 88.7% of every result byte the model read and the cap cuts the total by 88.4%.

Path counts, which now move only when the pin moves:

| pattern | live tree | pinned |
|---|---|---|
| `*` | 1,221 | 1,223 |
| `*.md` | 54 | 53 |
| `internal/*/*.go` | 203 | 206 |
| `bench/*/*.go` | 172 | 172 |
| `cmd/**/*.go` | 83 | 84 |
| `internal/*/*/*.go` | 148 | 148 |
| `catalog/**/*` | 23 | 23 |
| `interface/tui/*` | 33 | 33 |

## The cap band, restated on the pinned tree

Sorted by paths matched, the 136 calls that replay fall into three groups:

- 54 calls match nothing at all: a pattern that missed, costing 88 to 368 bytes each
- 71 calls match between 1 and 206: **206 is the largest scoped call in the corpus**, `internal/*/*.go`, and the next ones down are 172 for `bench/*/*.go` and 148 for `internal/*/*/*.go`
- 11 calls match more than 300, and all eleven are the bare pattern `*` at 1,223 paths

**The safe band is 207 to 1,222 and 300 sits inside it.** It is wider than the 204 to 347 the live tree reported, and wider for a reason worth naming rather than celebrating: the two calls that used to sit between 300 and 1,221, the ticket sweep at 348 and the `.local` sweep at 15,016, both named ignored paths and neither survives the pin. On tracked source alone there is nothing at all between 207 and 1,222, so the choice of cap in that range is unconstrained by this corpus and 300 is a judgement rather than a measurement. It is a judgement with 94 paths of margin above the largest scoped call.

Calls each candidate cap would cut:

| cap | calls cut, live tree | calls cut, pinned |
|---|---|---|
| 100 | 20 | 18 |
| 150 | 18 | 16 |
| 200 | 16 | 14 |
| 300 | 13 | 11 |
| 500 | 12 | 11 |

A cap of 100, which is what `.local/sources/opencode-dev/packages/opencode/src/tool/glob.ts:50` uses, costs seven scoped calls that 300 keeps whole, at 148, 172 and 206 paths. Raising to 500 recovers nothing at all now that the ticket sweep is out of the table.

**The cap is unchanged at 300. Nothing here argues for moving it and this bench does not move it.**

## Two runs, same tree, same counts

The command, run twice in a row on the same tree, with both outputs diffed:

```
$ go test ./bench/tokens/ -run TestByteTotalsOverTheRealCorpus -count=1 -v > /tmp/final1.txt 2>&1
$ go test ./bench/tokens/ -run TestByteTotalsOverTheRealCorpus -count=1 -v > /tmp/final2.txt 2>&1
$ diff /tmp/final1.txt /tmp/final2.txt
197c197
< --- PASS: TestByteTotalsOverTheRealCorpus (4.92s)
---
> --- PASS: TestByteTotalsOverTheRealCorpus (5.10s)
199c199
< ok  	tofu/bench/tokens	5.418s
---
> ok  	tofu/bench/tokens	5.737s
```

Two lines differ and both are wall clock. Every path count, every byte and every share is identical. The equivalent diff on the live tree differed on `*` within four minutes.

`TestTwoReplaysOfThePinnedTreeGiveTheSamePathCounts` makes the same check inside one process, comparing matched and returned for all 142 calls across two extractions.

## Bytes per tool, before the cap

```
before the glob cap
tool                calls          bytes    avg bytes     tokens    share
glob                  142       48186030       339338   12046507    88.7%
bash                  419        2408912         5749     602228     4.4%
read                  296        2268556         7664     567139     4.2%
grep                   47         840845        17890     210211     1.5%
artifact_fetch         71         328362         4624      82090     0.6%
search                 23         131752         5728      32938     0.2%
edit                   54          50993          944      12748     0.1%
project_report         37          47780         1291      11945     0.1%
plan                  169          25040          148       6260     0.0%
write                  83           3363           40        840     0.0%
tofu_lint_comments       29           2344           80        586     0.0%
tofu_rules_check       29           1305           45        326     0.0%
boji_rules_check        1             93           93         23     0.0%
boji_lint_comments        1             47           47         11     0.0%
```

## Bytes per tool, after the cap

```
after the glob cap
tool                calls          bytes    avg bytes     tokens    share
glob                  142         193346         1361      48336     3.1%
bash                  419        2408912         5749     602228    38.2%
read                  296        2268556         7664     567139    36.0%
grep                   47         840845        17890     210211    13.3%
artifact_fetch         71         328362         4624      82090     5.2%
search                 23         131752         5728      32938     2.1%
edit                   54          50993          944      12748     0.8%
project_report         37          47780         1291      11945     0.8%
plan                  169          25040          148       6260     0.4%
write                  83           3363           40        840     0.1%
tofu_lint_comments       29           2344           80        586     0.0%
tofu_rules_check       29           1305           45        326     0.0%
boji_rules_check        1             93           93         23     0.0%
boji_lint_comments        1             47           47         11     0.0%
```

```
total bytes: 54295422 before, 6302738 after, 88.4% saved
```

Tokens are bytes divided by `konst.SearchBytesPerToken`, which is 4. That is this project's own estimator and not a tokenizer: treat a token column as a scale, not a count.

**Dollars are $0.00 in every row, and this is a named skip rather than a result.** Every model in `catalog/models` carries `subscription: claude` or `subscription: openai` and no per-token rate, because a subscription spends a quota window instead of money. The catalog holds no rate card for any model, so the dollar column cannot be computed from it as it stands.

## The capped result, on the pinned tree

`glob` with the pattern `*`, real output, first twelve paths and the last four of three hundred, the 284 in between elided:

```
1223 of 1223 files under . match "*", showing the first 300
.gitattributes
.githooks/commit-msg
.githooks/commit-msg.ps1
.gitignore
.golangci.yml
CHANGELOG.md
README.md
bench/api/compare.go
bench/api/compare_live_test.go
bench/api/corpus.go
bench/api/corpus_test.go
bench/api/latency.go
...284 paths elided...
bench/transform/testdata/sessions/turn-18d69b4a2c474464.json
bench/transform/testdata/sessions/turn-18d69b4a7edc5228.json
bench/transform/testdata/sessions/turn-18d69b4aa36d9d2c.json
bench/transform/testdata/sessions/turn-18d69b4eeadf40e4.json
degraded truncated: a cap or a budget cut the answer short. 1223 files matched and 300 are shown: narrow the pattern to see the rest. this result is not a complete answer and must not be read as one
```

## Every recorded glob call and its path count

```
every recorded glob call, 142 of them, replayed against the tree at e3c805d2b86b5e289305f21a88bcde837b37c838
turn                        step    matched   returned  was bytes  now bytes  args
turn-18d6a7961d933ee8          1       1223        300        141        141  { "pattern": "*" }
turn-18d6a7961d933ee8          1          0          0         88         88  { "pattern": "src/*" }
turn-18d6a7e466fbd038          1       1223        300        141        141  { "pattern": "*" }
turn-18d6a7e466fbd038          1          0          0         88         88  { "pattern": "src/*" }
turn-18d6a8b2205e4268          1       1223        300        148        148  { "pattern": "*" }
turn-18d6a8b2205e4268          1          0          0         88         88  { "pattern": "src/*" }
turn-18d6d19d1553e404          1       1223        300    6302675      10340  { "pattern": "*" }
turn-18d6d19d1553e404          1         53         53     528958       1887  { "pattern": "*.md" }
turn-18d6d19d1553e404          2          4          4      63573        132  { "pattern": "README*" }
turn-18d6d9d1d2f26b20          1       1223        300    6304404      10340  { "pattern": "*" }
turn-18d6d9d1d2f26b20          1         53         53     529609       1887  { "pattern": "*.md" }
turn-18d6dc1528b6d81c          1       1223        300    6304648      10340  { "pattern": "*" }
turn-18d6dc1528b6d81c          1         53         53     529718       1887  { "pattern": "*.md" }
turn-18d6dc1528b6d81c          2          4          4      60647        134  { "pattern": "README.md" }
turn-18d6dc1528b6d81c          2          1          1       2895         46  { "pattern": "go.mod" }
turn-18d6dc1528b6d81c         10          0          0        885        148  { "pattern": ".local/boji/planning/*.md" }
turn-18d6dd1c12357e74          1       1223        300    6326832      10340  {"pattern":"*"}
turn-18d6dd1c12357e74          1         53         53     530181       1887  {"pattern":"*.md"}
turn-18d6dd1c12357e74          2         53         53     530181       1887  {"pattern":"*.md","path":"."}
turn-18d6de33a2c09dbc          1         53         53         49         49  {"pattern":"*.md"}
turn-18d6e194f179aa0c          1       1223        300    6328373      10340  {"pattern":"*"}
turn-18d6e194f179aa0c          1         53         53     530696       1887  {"pattern":"*.md"}
turn-18d6e1de4b235b54          1       1223        300    6328410      10340  {"pattern":"*"}
turn-18d6e3159a5e4610          1       1223        300    6328562      10340  {"pattern":"*"}
turn-18d6e3159a5e4610          4          0          0        916        148  {"pattern":".local/boji/planning/*.md"}
turn-18d6ea32da3230c0          1         53         53     531467       1887  {"pattern":"*.md"}
turn-18d6ed674fe16fac          1         53         53        613        613  {"pattern":"*.md"}
turn-18d6f0fb4994d0ac          2          0          0        136        136  {"pattern":".local/**/*.md"}
turn-18d6f0fb4994d0ac          3        206        206       3158       3158  {"pattern":"internal/*/*.go"}
turn-18d6f0fb4994d0ac          3         84         84       1306       1306  {"pattern":"cmd/**/*.go"}
turn-18d6f0fb4994d0ac          5          0          0       1264        355  {"pattern":".local/boji/planning/*.md","include_ignored":true}
turn-18d6f0fb4994d0ac          5        172        172       2361       2361  {"pattern":"bench/*/*.go"}
turn-18d6f0fb4994d0ac          5         23         23       1520        793  {"pattern":"catalog/**/*"}
turn-18d6f0fb4994d0ac          8         33         33        517        517  {"pattern":"interface/tui/*"}
turn-18d6f0fb4994d0ac          9        148        148       3720       3720  {"pattern":"internal/*/*/*.go"}
turn-18d6f22be5cb6180          2          0          0        136        136  {"pattern":".local/**/*.md"}
turn-18d6f22be5cb6180          3          0          0       1303        355  {"pattern":".local/boji/planning/*.md","include_ignored":true}
turn-18d6f22be5cb6180          3          0          0        132        132  {"pattern":"internal/*"}
turn-18d6f49819dff224          2         53         53        631        631  {"pattern":"*.md"}
turn-18d6f49819dff224          2          0          0       1333        136  {"pattern":"cmd/boji/*.go"}
turn-18d6f49819dff224          4          0          0        132        132  {"pattern":"internal/*"}
turn-18d6f49819dff224          9          0          0        296        296  {"pattern":".local/boji/planning/*.md","include_ignored":true}
turn-18d6f8d9e45f8efc          2         53         53        631        631  {"pattern":"*.md"}
turn-18d6f8d9e45f8efc          3          0          0        132        132  {"pattern":"internal/*"}
turn-18d6f8d9e45f8efc          6         84         84       1400       1400  {"pattern":"cmd/tofu/*.go"}
turn-18d6f8d9e45f8efc          8          0          0       1138        344  {"pattern":".local/**/*.md","include_ignored":true}
turn-18d7240c1d7291dc          1          0          0        155        155  {"pattern":"*.{png,jpg,jpeg,gif,webp,svg,bmp}"}
turn-18d7240c1d7291dc          2          0          0        368        367  {"pattern":"*.{png,jpg,jpeg,gif,webp,svg,bmp,ico}","include_ignored":true}
turn-18d724d865ed9264          1         53         53       1409       1409  {"pattern":"*.md"}
turn-18d724d865ed9264          2          0          0        132        132  {"pattern":"internal/*"}
turn-18d724d865ed9264          2         84         84       1536       1536  {"pattern":"cmd/tofu/*.go"}
turn-18d724d865ed9264         13          0          0      10487        357  {"pattern":".local/boji/tickets/**/*.md","include_ignored":true}
turn-18d72fd10b45a474          1         53         53       1409       1409  {"pattern":"*.md"}
turn-18d72fd10b45a474          9          0          0        188        188  {"pattern":"internal/{crew,recall,sift,search,web,transport,session,rule}/*.go"}
turn-18d72fd10b45a474         11        206        206       5045       5045  {"pattern":"internal/*/*.go"}
turn-18d732c5a61bd6ac          3          0          0        685        146  {"include_ignored":false,"path":"","pattern":"internal/judge/policy/*"}
turn-18d732e1a8382e74          4    skipped    skipped        470        470  {"include_ignored":false,"path":"internal/judge/policy","pattern":"*_test.go"}: replay failed: glob: internal/judge/policy is not a path under the working directory, and nothing there is named policy: nothing was run
turn-18d732f3ab8b7b28          4    skipped    skipped        470        470  {"include_ignored":false,"path":"internal/judge/policy","pattern":"*_test.go"}: replay failed: glob: internal/judge/policy is not a path under the working directory, and nothing there is named policy: nothing was run
turn-18d7330f2485f2a4          3         21         21        719        719  {"include_ignored":false,"path":"internal/judge/ledger","pattern":"*"}
turn-18d7330f2485f2a4          3          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/AGENTS.md"}
turn-18d7330f2485f2a4          3          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/CLAUDE.md"}
turn-18d7330f2485f2a4          5          1          1         56         56  {"include_ignored":false,"path":"","pattern":".golangci*"}
turn-18d7330f2485f2a4         15          1          1        100        100  {"include_ignored":false,"path":"internal/judge/ledger","pattern":"spend_test.go"}
turn-18d7336dfa89c818          4         21         21        719        719  {"include_ignored":false,"path":"internal/judge/ledger","pattern":"*"}
turn-18d7336dfa89c818          4         55         55       2052       2052  {"include_ignored":false,"path":"internal/judge","pattern":"*_test.go"}
turn-18d733d413c55f34          3          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/AGENTS.md"}
turn-18d733d413c55f34          3          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/CLAUDE.md"}
turn-18d733d413c55f34          4          0          0        147        147  {"include_ignored":false,"path":"internal","pattern":"**/*policy_test.go"}
turn-18d733d413c55f34          4          8          8        251        251  {"include_ignored":false,"path":"internal/crew","pattern":"*_test.go"}
turn-18d733d413c55f34          5          0          0        135        135  {"include_ignored":false,"path":"catalog","pattern":"**/*ask*"}
turn-18d733d413c55f34          6         16         16        464        458  {"include_ignored":false,"path":"internal/sift","pattern":"*.go"}
turn-18d733d413c55f34          6    skipped    skipped        334        334  {"include_ignored":false,"path":"catalog/policy","pattern":"*.yaml"}: replay failed: glob: catalog/policy is not a path under the working directory, and nothing there is named policy: nothing was run
turn-18d733f0d52edb34          3          0          0        153        153  {"include_ignored":false,"path":"","pattern":"**/{AGENTS.md,CLAUDE.md,RTK.md}"}
turn-18d733f0d52edb34          4          8          8        251        251  {"include_ignored":false,"path":"internal/crew","pattern":"*_test.go"}
turn-18d733f0d52edb34          4          0          0        135        135  {"include_ignored":false,"path":"catalog","pattern":"**/*ask*"}
turn-18d733f0d52edb34          5          0          0        149        149  {"include_ignored":false,"path":"internal/sift","pattern":"*policy*_test.go"}
turn-18d733f0d52edb34          9    skipped    skipped        721        721  {"include_ignored":false,"path":"internal/judge/policy","pattern":"*.go"}: replay failed: glob: internal/judge/policy is not a path under the working directory, and nothing there is named policy: nothing was run
turn-18d734166608ead0          3          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/AGENTS.md"}
turn-18d734166608ead0          3          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/CLAUDE.md"}
turn-18d734166608ead0          3         19         19        531        531  {"include_ignored":false,"path":"internal/recall","pattern":"*.go"}
turn-18d734590b989e04          4         22         22        560        560  {"include_ignored":false,"path":"internal/recall","pattern":"*"}
turn-18d734590b989e04          5          0          0        141        141  {"include_ignored":false,"path":"","pattern":"**/*catalog_test.go"}
turn-18d734967b349e38          3         19         19        765        765  {"include_ignored":false,"path":"internal/judge/state","pattern":"*"}
turn-18d734b19e9a8a34          3          0          0        153        153  {"include_ignored":false,"path":"","pattern":"**/{AGENTS.md,CLAUDE.md,RTK.md}"}
turn-18d734b19e9a8a34          3         19         19        765        765  {"include_ignored":false,"path":"internal/judge/state","pattern":"*"}
turn-18d734de323da498          4         26         26        859        859  {"include_ignored":false,"path":"internal/judge/jev","pattern":"*"}
turn-18d734de323da498          4         55         55       2052       2052  {"include_ignored":false,"path":"internal/judge","pattern":"*_test.go"}
turn-18d734f37feab3ec          4         26         26        859        859  {"include_ignored":false,"path":"internal/judge/jev","pattern":"*"}
turn-18d7351543206e44          4          7          7        246        246  {"include_ignored":false,"path":"internal/session","pattern":"*_test.go"}
turn-18d7352aba41fa14          4          0          0        153        153  {"include_ignored":false,"path":"","pattern":"**/{AGENTS.md,CLAUDE.md,RTK.md}"}
turn-18d7352aba41fa14          4         20         20        697        697  {"include_ignored":false,"path":"internal/session","pattern":"*"}
turn-18d73551129b3218          3          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/AGENTS.md"}
turn-18d73551129b3218          3          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/CLAUDE.md"}
turn-18d73551129b3218          4          3          3        120        120  {"include_ignored":false,"path":"internal/settings","pattern":"*_test.go"}
turn-18d73551129b3218          4          7          7        225        225  {"include_ignored":false,"path":"internal/settings","pattern":"*.go"}
turn-18d73582f35a0ac8          4          7          7        226        226  {"include_ignored":false,"path":"","pattern":"internal/settings/*"}
turn-18d73582f35a0ac8          4          3          3        124        124  {"include_ignored":false,"path":"","pattern":"internal/settings/*_test.go"}
turn-18d73582f35a0ac8          4          0          0        131        131  {"include_ignored":false,"path":"","pattern":"AGENTS.md"}
turn-18d73582f35a0ac8          4          0          0        131        131  {"include_ignored":false,"path":"","pattern":"CLAUDE.md"}
turn-18d735acea2a3d54          3         12         12        442        442  {"include_ignored":false,"path":"","pattern":"internal/judge/question/*"}
turn-18d735bfc57c5150          3          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/AGENTS.md"}
turn-18d735bfc57c5150          3          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/CLAUDE.md"}
turn-18d735bfc57c5150          4          5          5        214        214  {"include_ignored":false,"path":"internal/judge/question","pattern":"*_test.go"}
turn-18d735e1c1059e34          3          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/AGENTS.md"}
turn-18d735e1c1059e34          3          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/CLAUDE.md"}
turn-18d735e1c1059e34          4          6          6        155        155  {"include_ignored":false,"path":"internal/sys","pattern":"*_test.go"}
turn-18d735fadaabbcc4          4          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/AGENTS.md"}
turn-18d735fadaabbcc4          4          0          0        134        134  {"include_ignored":false,"path":"","pattern":"**/CLAUDE.md"}
turn-18d735fadaabbcc4          4         15         15        357        357  {"include_ignored":false,"path":"internal/sys","pattern":"*"}
turn-18d735fadaabbcc4          4          6          6        155        155  {"include_ignored":false,"path":"internal/sys","pattern":"*_test.go"}
turn-18d735fadaabbcc4          6          1          1         51         51  {"include_ignored":false,"path":"","pattern":"*.yml"}
turn-18d735fadaabbcc4          6         57         57       1975       1975  {"include_ignored":false,"path":"","pattern":"*.yaml"}
turn-18d73a4d4a8f4564          3    skipped    skipped        682        682  {"include_ignored":false,"path":"internal/judge/policy","pattern":"*"}: replay failed: glob: internal/judge/policy is not a path under the working directory, and nothing there is named policy: nothing was run
turn-18d73a4d4a8f4564          3          0          0        150        150  {"include_ignored":false,"path":"","pattern":"{AGENTS.md,CLAUDE.md,RTK.md}"}
turn-18d73aaceac859f8          4         21         21        719        719  {"include_ignored":false,"path":"internal/judge/ledger","pattern":"*"}
turn-18d73ae22706ecec          4          8          8        251        251  {"include_ignored":false,"path":"internal/crew","pattern":"*_test.go"}
turn-18d73ae22706ecec          4          0          0        128        128  {"include_ignored":false,"path":"","pattern":"RTK.md"}
turn-18d73ae22706ecec          4          0          0        131        131  {"include_ignored":false,"path":"","pattern":"AGENTS.md"}
turn-18d73ae22706ecec          5          2          2         97         97  {"include_ignored":false,"path":"","pattern":"*ask*.yaml"}
turn-18d73aefd9f4a9e0          4         19         19        765        765  {"include_ignored":false,"path":"internal/judge/state","pattern":"*"}
turn-18d73aefd9f4a9e0          4         55         55       2051       2051  {"include_ignored":false,"path":"internal/judge","pattern":"*_test.go"}
turn-18d7404cb55ab0f4          2         53         53       1410       1410  {"pattern":"*.md"}
turn-18d7404cb55ab0f4          2          0          0        133        133  {"pattern":"internal/*"}
turn-18d7404cb55ab0f4          3        206        206       5157       5157  {"pattern":"internal/**/*.go"}
turn-18d7404cb55ab0f4          4         84         84       1709       1709  {"pattern":"cmd/**/*.go"}
turn-18d7404cb55ab0f4          6         33         33        817        817  {"pattern":"interface/**/*.go"}
turn-18d7404cb55ab0f4          7        148        148       5011       5011  {"pattern":"internal/*/*/*.go"}
turn-18d7404cb55ab0f4          7        172        172       4129       4129  {"pattern":"bench/*/*.go"}
turn-18d742bd38c430c4          2       1223        300      10196      10196  {"pattern":"*"}
turn-18d742bd38c430c4          3    skipped    skipped      11689      11689  {"pattern":"*.md","path":".local","include_ignored":true}: replay failed: glob: .local is not a path under the working directory, and nothing there is named .local: nothing was run
turn-18d742bd38c430c4          4         84         84       1711       1711  {"pattern":"cmd/tofu/*.go"}
turn-18d742bd38c430c4          4          0          0        133        133  {"pattern":"internal/*"}
turn-18d7430fd0c0c304          2         53         53       1410       1410  {"pattern":"*.md"}
turn-18d7430fd0c0c304          2         84         84       1709       1709  {"pattern":"cmd/**/*.go"}
turn-18d7430fd0c0c304          3          0          0        134        134  {"pattern":"internal/*/"}
turn-18d7474e7fae090c          1         53         53       1472       1472  {"pattern":"*.md"}
turn-18d7474e7fae090c          2         84         84       1711       1711  {"pattern":"cmd/tofu/*.go"}
turn-18d7474e7fae090c          2          0          0        133        133  {"pattern":"internal/*"}
turn-18d7474e7fae090c          3          0          0        134        134  {"pattern":"internal/*/"}
turn-18d7474e7fae090c          4          0          0        142        142  {"pattern":"internal/**/doc*.go"}
turn-18d74adff1dccf60          2         53         53       1472       1472  {"pattern":"*.md"}
turn-18d74adff1dccf60          2         84         84       1709       1709  {"pattern":"cmd/**/*.go"}
```

`matched` and `returned` are a replay of the recorded arguments against the tree at `e3c805d`. `was bytes` is the real recorded result size from the session file. `now bytes` is the smaller of the replayed capped size and the recorded size.

## The corpus, stated once

`.tofu/sessions` held 110 entries at 20:45 on 2026-09-21 and 109 of them read as sessions. The one entry that is not a session is `HEAD`, a pointer file.

The corpus is live and grows while the bench reads it, so a session count in any report over it is a count at a named minute. The call and byte totals are stable and the session count is not. Nothing in this run writes to `.tofu`: the ledger stood at 2,999 rows before the first run and 2,999 after the last.

The tree is now a separate matter from the corpus, which is the change this ticket makes. A new turn recorded during a run still moves the session count. It can no longer move a path count.

## What this does not settle

**The 88.4% is a bound, not a clean measurement of the cap alone.** The eleven `*` calls recorded at about 6.3 MB each ran in a working directory that is not this repository, and replaying them at `e3c805d` gives 10,340 bytes against a tree of 1,223 files. The drop for those calls mixes the cap with the difference between two trees.

**The pin freezes the decay, it does not repair it.** Six calls cannot replay and that number is now a constant a test defends rather than a figure that creeps. It will only change when someone changes the pin, and changing the pin is a decision with a report attached.

**A pinned tree is a committed tree, so ignored paths are unmeasurable here.** The corpus contains real calls over `.local` and this bench can no longer say what they cost. Measuring those needs a different instrument, one that records the matched count at the time of the call rather than replaying the arguments later.

**Which 300 of the 1,223 paths should come back is not measured here and cannot be**, because until this cap existed the answer was always all of them. The free rival for that judgment is the sort order, which is currently the walk order.

**`read` at 2,268,556 bytes over 296 calls and `bash` at 2,408,912 over 419 are within 6.2% of each other.** Neither is dominated by a handful of huge results the way glob was, so the next saving is a per call one and will be smaller than this one by an order of magnitude.
