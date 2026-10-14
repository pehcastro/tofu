#!/usr/bin/env bash
set -u
here=$(cd "$(dirname "$0")" && pwd)
bin=${MINIMAL_BIN:?set MINIMAL_BIN to the directory build.sh wrote}
work=${MINIMAL_WORK:?set MINIMAL_WORK to a scratch directory on the Linux file system}
limits=${MINIMAL_LIMITS:-"256 128"}
rungs=${MINIMAL_RUNGS:-"0 1 4 10"}
floors=${MINIMAL_FLOORS:-"64 48 32 24"}
results=${MINIMAL_RESULTS:-$here/results.jsonl}
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin GOMAXPROCS=2

rm -rf "$work"
mkdir -p "$work"
for name in tofu-linux tofu-linux-head scale-linux minimal-linux; do install -m 755 "$bin/$name" "$work/$name"; done
built="$("$work/tofu-linux" version | awk '$1 == "version" { print $2 }') $(cat "$bin/commit")"
project=$work/project
"$work/scale-linux" fixture "$project"

cold() { sync && echo 3 > /proc/sys/vm/drop_caches; }

tofu_in_limit() {
  local label=$1 tool=$2 memory=$3 cpu=$4 agents=$5 binary=$6
  shift 6
  local home procs=0 deck=$work/cassette-$tool-$agents.jsonl
  [ "$cpu" -lt 0 ] && procs=2
  home=$(mktemp -d "$work/home-XXXX")
  "$work/scale-linux" cassette "$project" "$agents" "$deck"
  [ "$tool" = bash ] && sed -i -E 's/\{"name":"read","args":\{"path":"(f[0-9]+\.txt)"\}\}/{"name":"bash","args":{"command":"head -n 1 \1"}}/g' "$deck"
  cold
  HOME=$home TOFU_DRIVE_CASSETTE=$deck TOFU_MINIMAL_COUNT=$work/count.json "$@" \
    "$work/minimal-linux" sample -tool "$label" -version "$built" -agents "$agents" -memory "$memory" -cpu "$cpu" -gomaxprocs "$procs" -count "$work/count.json" \
    -project "$project" -out "$results" -transcript "$work/$label-$memory-$agents.txt" -- \
    "$binary" run --dir "$project" --model claude-sub/claude-sonnet-5 --no-instructions --no-gate --sift free "spawn $agents"
  rm -rf "$home" "$work/count.json"
}

for memory in $limits; do
  for agents in $rungs; do
    tofu_in_limit tofu read "$memory" 1 "$agents" "$work/tofu-linux"
  done
  for agents in $rungs; do
    tofu_in_limit tofu-bash bash "$memory" 1 "$agents" "$work/tofu-linux"
  done
  home=$(mktemp -d "$work/home-XXXX")
  cold
  HOME=$home TOFU_MINIMAL_COUNT=$work/count.json "$work/minimal-linux" sample -tool tofu -task serve-idle -hold 20s -timeout 40s -version "$built" -memory "$memory" -cpu 1 \
    -count "$work/count.json" -project "$project" -out "$results" -transcript "$work/serve-$memory.txt" -- "$work/tofu-linux" serve --stdio
  rm -rf "$home" "$work/count.json"
done

for agents in 0 10; do
  tofu_in_limit tofu-no-overlay read 256 1 "$agents" "$work/tofu-linux-head"
  tofu_in_limit tofu-no-limit read 0 -1 "$agents" "$work/tofu-linux"
  for memory in $floors; do
    tofu_in_limit tofu-floor read "$memory" 1 "$agents" "$work/tofu-linux"
  done
done

for tool in read bash; do
  for agents in 1 4 10; do
    rm -rf "$work/pprof-$tool-$agents"
    tofu_in_limit "tofu-pprof-$tool" "$tool" 256 1 "$agents" "$work/tofu-linux" env TOFU_PPROF="$work/pprof-$tool-$agents"
  done
done
"$work/minimal-linux" report "$results"
