## Method

| item | what it is |
|---|---|
| task | one small Go package, `testdata/seed`, and one prompt, `testdata/prompt.txt`: a bug, a feature, a rename, a question and a new file |
| quality | hidden tests from `testdata/hidden` plus the rubric in `score.go`: visible tests, gofmt, go vet, the old name gone, no stray files |
| verdict | a win or a loss only when the lowest to highest spreads of the two arms do not meet; otherwise a tie |
| order | one run at a time behind the rival slot, on the owner's subscriptions, no money |
| network | this bench needs the network and spends subscription quota; the report itself reads only `runs/` |
| rerun | `go run ./bench/rivals run -arm <arm> -rep <n> -setting omitted -batch <batch> -slot <dir>`, then `go run ./bench/rivals report -batch <batch>` |

## Parity

Read off the rival's own stdout or rollout of the first run of each arm; the models and the thinking tofu sent are in the conditions table.

| arm | effort, and how it was set | thinking summary, and what shows it |
|---|---|---|
| tofu-sonnet | medium, `--effort medium` | none, `--thinking-summary omitted` |
| claude-sonnet | medium, `--effort medium` | none: Claude Code sends `display` only when `showThinkingSummaries` is set, it is not, and its thinking came back empty |
| omp-sonnet | medium, `--thinking medium` | none: `omitThinking: true` in `testdata/omp-overlay.yml`, and its thinking came back empty |
| tofu-gpt | medium, `--effort medium` | none, `--thinking-summary omitted` |
| codex-gpt | medium, `-c model_reasoning_effort="medium"` | none: the rollout `turn_context` says `summary: none` |

## Caveats

| measure | caveat |
|---|---|
| memory model calls | counted only in batch runs; the earlier runs did not record them |
| time to first token | codex's first token is its first finished output item, so its figure is an upper bound |
| tool time | tofu times each call itself; Claude Code, omp and codex are timed from the call event on stdout to its result |
| edit call p50 | the median edit call of each run; Claude Code edits through bash, so it has no edit row |
