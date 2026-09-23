# Where shell-corpus.jsonl came from

`shell-corpus.jsonl` is 34 shell results this harness really returned to a model, copied out of `.tofu/sessions` on 2026-09-20 for TOFU-217. Nothing in `.tofu/sessions` was changed, moved or deleted to make it.

## How it was taken

Every `body.jsonl` under `.tofu/sessions` was read. A record with `kind: message` and `role: assistant` names the tool for each `tool_call_id`; the matching record with `role: tool` carries that call's result whole. Every result whose call was `bash` was taken, and results identical byte for byte were kept once, because a forked session replays its parent's results. 48 bash results reduce to 34 distinct ones, 108,396 bytes.

`task` is the user's own request from the session's `header.json`, not a task anybody wrote for this measurement. Four distinct tasks appear across the 34 rows, and three of them are a form of asking what this repository is, which is the largest known weakness of the corpus: it is one person, one repository and a narrow band of questions.

## What was removed before it was written here

Every field of every row went through `bench/corpus.Scrub`, which is what the other 177 recorded fixtures in this tree go through. Two rows changed, 21 bytes in 108,396:

- Row 5, a `head -70 .local/boji/tickets/BOARD.md`, captured a board entry naming the owner's home directory in an install path. The account name became `owner`, +1 byte.
- Row 8, a `sed -n '1,40p' .local/boji/planning/connectors.md`, captured the line of that document which says how an Anthropic OAuth token is recognised, so it quotes the prefix a scanner looks for. It is the detector rather than a credential, and it is masked anyway, because a scanner that learns an exception is worth less than one that is blunt. +20 bytes.

Neither substitution adds or removes a newline, so no unit boundary moved and the needle each arm is scored on sits in the same unit index as before.

`ReadCorpus` refuses any row that `corpus.LeaksIn` fires on, so a re-extraction that skips the scrub fails the measurement rather than quietly shipping.

## What a row is

One JSON object per line: `session`, `task`, `command`, `outcome` and `output`. `outcome` is the harness's own word for the call, `ran` or `failed`.

## What it does not carry

`internal/turn/tool_bash.go` runs a command with `CombinedOutput`, so standard error and standard output arrive interleaved in one stream and no recorded row can say which line came from which. Every row is therefore loaded as standard output with the exit status taken from `outcome`. The rule that standard error is never a candidate for removal is proved on constructed input in `internal/sift/shell_test.go` instead, and it cannot be proved on this corpus until the tool separates the two streams.

# Where judged-replies.jsonl came from

**It is not a recording, and it says so here because the reason it is not one is the bug TOFU-415 fixes.** Every jev reply this bench ever paid for was thrown away at the end of the run, so on 2026-09-22 there was nothing on disk to replay. The six lines are written by hand in the shape `jev.Decode` accepts, carrying the build id `typesafe/jev-1.13-20260917` that the 2026-09-21 and 2026-09-22 runs both reported, and a per-call cost of $0.0000306, which is the $0.00909 of a 297 call run divided by its calls.

`record_test.go` serves them round robin, one per call, so the judged arm runs offline over the whole corpus and leaves 297 rows without a network call or a cent. Only the row count, the row's shape and the state it carries are proved that way. No figure about how well the sieve scores can come from this file.

Once a paid run has written rows under `.tofu/bench/sift/log`, a real reply can replace these six, and the day it does this section says so.
