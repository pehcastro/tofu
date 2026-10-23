#!/usr/bin/env bash
set -u
here=$(cd "$(dirname "$0")" && pwd)
tofu=${DRAIN_TOFU:?set DRAIN_TOFU to the tofu binary measured}
id=${DRAIN_SESSION:?set DRAIN_SESSION to the session id drained}
warm=${DRAIN_WARM:-5}
cd "${DRAIN_DIR:-$here/../..}" || exit 1
results=$here/results.jsonl
day=$(date +%F)
machine=$(hostname)

events=$(find "$HOME/.tofu/projects" -path "*/sessions/$id/events.jsonl" | head -1)
[ -n "$events" ] || { echo "no events.jsonl for $id under ~/.tofu/projects" >&2; exit 1; }
bytes=$(stat -c %s "$events")
commit=$("$tofu" version | awk '$1 == "commit" { print $2 }')
[ -n "$commit" ] || exit 1

clock() {
  local start end
  start=$(date +%s%N)
  "$tofu" "$@" > /dev/null || exit 1
  end=$(date +%s%N)
  echo $(( (end - start) / 1000000 ))
}

measure() {
  local arm=$1 run=$2 ms lines
  shift 2
  ms=$(clock "$@") || exit 1
  lines=$(wc -l < "$events")
  printf '{"date":"%s","machine":"%s","commit":"%s","session":"%s","bytes":%s,"events":%s,"arm":"%s","run":%s,"ms":%s}\n' \
    "$day" "$machine" "$commit" "${DRAIN_LABEL:-recorded}" "$bytes" "$lines" "$arm" "$run" "$ms" | tee -a "$results"
}

measure cold 1 session drain "$id"
for run in $(seq 1 "$warm"); do measure warm "$run" session drain "$id"; done
for run in $(seq 1 "$warm"); do measure floor "$run" version; done
