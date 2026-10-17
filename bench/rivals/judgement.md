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

## Fresh input, by source

Fresh input is the prompt the provider served from no cache. Split per request from `tofu session trace` of each run's work folder and, for codex, the `last_token_usage` of its rollout; every total equals the run file's `fresh_in`.

| source | tofu-sonnet runs 1, 2, 3 | tofu-gpt runs 1, 2, 3 | codex-gpt runs 1, 2, 3 | does it earn its cost |
|---|---|---|---|---|
| conversation, first request | 4, 4, 4 | 7,556, 7,556, 7,556 | 3,142, 4,142, 4,142 | Sonnet reads the prefix the previous run cached. On GPT tofu's prefix is cold every session while codex's first request reads 11,392 cached; a guess to test: `prompt_cache_key` is the session id, `internal/llm/wire/codex/request.go:159` |
| conversation, messages new since the last request | 8, 6, 8 | 5,727, 5,970, 6,396 | 7,535, 8,071, 6,195 | yes: tool results the model has not seen yet, about what codex sends |
| conversation, a later request with no cache read | 0, 0, 0 | 12,890 at step 7, 12,668 at step 6, 0 | 0, 0, 0 | no, waste: the instructions, tools and first message were byte-identical to the first request of the run, checked on run 1, and the request before it did read the cache, so the cause is not found in the body; needs a ticket |
| memory model, claude-haiku-4-5 | 2,766, 4,167, 2,772 | 2,724, 2,706, 3,209 | none | tofu's own feature: three one-shot calls of about 900 tokens, none read from cache; whether memory pays is the memory bench's question, not this one |
| total, the run file's fresh input | 2,778, 4,177, 2,784 | 28,897, 28,900, 17,161 | 10,677, 12,213, 10,337 | on Sonnet the conversation alone is 10 to 12, level with Claude Code's 6 and omp's 12 |

## Caveats

| measure | caveat |
|---|---|
| kept out of the comparison | memory model calls, time to first token, edit call p50 and cache write: not every arm can have them |
| tool time | tofu times each call itself; Claude Code, omp and codex are timed from the call event on stdout to its result |
| CPU use | CPU seconds of the whole process tree over wall seconds, so cores busy on average |
| load | the load tables come from earlier benches, `bench/scale` and `bench/longsession`, on older builds; their dates are in the rows |
