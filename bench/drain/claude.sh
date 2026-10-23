#!/usr/bin/env bash
set -u
here=$(cd "$(dirname "$0")" && pwd)
tofu=${DRAIN_TOFU:?set DRAIN_TOFU to the tofu binary measured}
transcript=${DRAIN_CLAUDE:?set DRAIN_CLAUDE to a Claude Code session jsonl}
runs=${DRAIN_RUNS:-2}
results=$here/results.jsonl
day=$(date +%F)
machine=$(hostname)
procs=${GOMAXPROCS:?set GOMAXPROCS so the run is comparable}

subagents=${transcript%.jsonl}/subagents
main_bytes=$(stat -c %s "$transcript")
sub_files=$(find "$subagents" -name 'agent-*.jsonl' 2>/dev/null | wc -l)
sub_bytes=$(find "$subagents" -name 'agent-*.jsonl' -exec stat -c %s {} + 2>/dev/null | awk '{ s += $1 } END { print s + 0 }')
commit=$("$tofu" version | awk '$1 == "commit" { print $2 }')
[ -n "$commit" ] || exit 1

for run in $(seq 1 "$runs"); do
  start=$(date +%s%N)
  "$tofu" session drain --claude "$transcript" > /dev/null || exit 1
  end=$(date +%s%N)
  printf '{"date":"%s","machine":"%s","commit":"%s","session":"claude:%s","main_bytes":%s,"subagent_files":%s,"subagent_bytes":%s,"gomaxprocs":%s,"arm":"claude","run":%s,"ms":%s}\n' \
    "$day" "$machine" "$commit" "$(basename "$transcript")" "$main_bytes" "$sub_files" "$sub_bytes" "$procs" "$run" $(( (end - start) / 1000000 )) | tee -a "$results"
done
