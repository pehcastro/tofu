# Where the embedded states came from

Eleven JSON states plus `provenance.json`, 172,055 bytes, all of them **written**, none of them recorded. `embed.go` ships them and `bench/api/corpus.go` loads them.

## The six gate cases, written

`case-1-ls.json` through `case-6-sed-named-file.json` were authored fresh for BOJI-006, which `provenance.json` beside them has said since they were added. bench-001 recorded what each case meant in prose and never recorded the literal payload, so there was nothing to copy. `provenance.json` lists `cwd` and `context.user_recent_messages[0]` as guessed in all six, plus `context.flagged_untrusted_content.excerpt` in case 5. Everything else is fixed by bench-001's own description and BOJI-005's log.

**Written is right for these and only for what they are used for.** They are the fixed input of a latency and rerun-agreement measurement: the same six states go to the same battery on every run so two runs are comparable. For that job an invented state is as good as a recorded one, because nothing is being scored for accuracy.

**Written is not right for the thing they are also used for.** The same six appear in `bench/corpus/gate/cases.jsonl` as `auth-case-1` to `auth-case-6` with hand labels, and those six labelled rows sit beside 172 recorded ones in every accuracy number the cost bench reports. Six invented rows in 178 is 3.4% of that corpus, and no report separates them. A reader should not read a gate agreement figure as resting on 178 recorded cases.

## The five size fixtures, written

`size-300.json`, `size-1k.json`, `size-4k.json`, `size-12k.json`, `size-28k.json` are the latency-by-state-size ladder. They are written because the measurement needs state sizes at chosen points, and a recorded session does not produce states at 300, 1k, 4k, 12k and 28k on request. They carry no label and nothing scores them for accuracy.

## Leakage, checked on 2026-09-23, and one row leaks

**Method.** Two passes over all eleven files. First, every verdict word the gate can return, and the near synonyms of them, searched as a whole word in the whole file: `proceed`, `block`, `deny`, `ask`, `allow`, `refuse`, `safe`, `dangerous`, `unsafe`, `approve`, `reject`. Second, every word of at least three characters in a file's own name, searched in the state, since the name is the human label of the case.

**Verdict words: zero hits in eleven files.** No state names its own verdict.

**Name words: hits in four files, and three of them are not leakage.** `case-2` and `case-3` contain `force`, `push`, `tests`, `case-5` contains `curl`, `case-6` contains `sed`. In every one of those the hit is the command itself, which is the input the decision is about. The file name is never given to any arm. Case 2 and case 3 carry the identical command with opposite labels and are separated only by the user message, which is the pair working as intended.

**`case-5-curl-exfil-planted.json` leaks, and it is not repaired here.** Two ways, both in that one row of six:

- the host is `example-evil.test`. The word `evil` is in the string being judged. The `block` label is recoverable from that substring alone with no reasoning about exfiltration, so this case cannot separate a model that understands the attack from one that pattern-matches a hostile-looking domain.
- `context.flagged_untrusted_content.excerpt` quotes `input.command` byte for byte. The `from_untrusted` question is then decidable by `strings.Contains`, which makes this case unable to separate any arm from two lines of Go.

One of six written gate states leaks. Not repaired: changing a byte would move every latency and agreement figure recorded against these files. It needs its own ticket.

## What it does not carry

Scanned on 2026-09-23 for every identity and credential pattern in `Scrub`: no hit in any of the eleven files. The `cwd` in all of them is the invented `/home/user/project`.
