# Where questions.jsonl came from

**Written, 98 of 100**, and each row says so in its own `source` field: 98 `written`, 2 `recorded` from turns in `.tofu/sessions`. 25,694 bytes, added 2026-09-21, six answers repinned on 2026-09-22 by TOFU-404 and a seventh on 2026-09-23 by TOFU-462. On 2026-10-05 TOFU-1083 repinned `tofu-09` from `glob.go:124` to `glob.go:90`, because f029a81 deleted `matchesPattern` and the whole path or last segment choice moved into `Glob.Run`, and moved 16 line hints to where their unchanged fingerprints had already followed.

**Written is right for this one, and it is the clearest case in `bench/`.** The corpus it replaced was the argument for it: a 30 question set, of which **25 named a component of their own answer**, on which three arms all scored 14 of 30. The tie measured the corpus, not the tools. A question that does not name its answer has to be composed against a rule, and no recorded session produces one by accident: a person asking a real question names the file they are looking for.

Five trees, 20 questions each: `tofu`, `codex-main`, `cline`, `goose`, `gemini-cli`. Three bands, and the bands are the design:

- **named**, 20. Carries a backticked identifier that appears on its own answer line. Easy by construction. It is the control: an arm that cannot do named is broken.
- **described**, 40. Says what the code does in other words.
- **intent**, 40. Asks why the code is the way it is.

Each answer is a file and a line, chosen by reading the source. **No arm output was consulted when picking a line.** That is the only defence against fitting the corpus to the arms scored on it, and it is a promise rather than a mechanism.

## Leakage: checked by a test that runs on every suite run, zero found

**Method**, `bench/tools/corpus/discipline_test.go`, which is the mechanism this whole ticket asks other corpora to copy. For every described and intent question it splits every path component, file stem and declared identifier of that question's own answer into words of three characters or more, on non-alphanumerics and on camel case, and fails if any of them appears in the question's content words.

**Count: zero of 80 eligible questions.** Zero were excluded to get there: the exclusion happened when the corpus was built, and the 30 question set that failed this rule was deleted rather than trimmed.

**The 20 named questions leak by construction and are exempt from the test.** That is what the band is for, and it is why the named row is read as a control rather than as a score.

## What a reader should not conclude

**The corpus rots.** Six answers pointed at lines that had moved or at a file TOFU-284 deleted, and three of them were scoring zero for every arm, which is a broken control rather than a hard question. Each of the six carries a `note` saying what it was. A seventh, `tofu-07`, pointed into the file TOFU-459 shrank from 70 lines to 7 when it moved the credential detector to `internal/secret`. It was caught only because the file shrank past the cited line: **82 of the 102 answers carry no quoted term**, so a rewrite under one of those is silent. Repinned twice already; it will happen again.

**It does not separate the arms it was built for.** `git grep` at 29 of 100 against `tofu search` at 18 is p = 0.10, not separable. The one clean result is `bm25 alone` at 0 of 20. Nothing here ranks `file_shortlist`.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `internal/secret.Scrub`: no hit.
