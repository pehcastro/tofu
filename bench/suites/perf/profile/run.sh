#!/usr/bin/env bash
set -u
here=$(cd "$(dirname "$0")" && pwd)
root=$(git -C "$here" rev-parse --show-toplevel)
work=${PROFILE_WORK:?set PROFILE_WORK to a scratch directory outside any repository}
slot=${PROFILE_SLOT:?set PROFILE_SLOT to the directory taken with mkdir so one model run holds the machine}
live=${PROFILE_LIVE:-yes}
tofu=${PROFILE_TOFU:-$work/tofu.exe}

native() { if command -v cygpath >/dev/null; then cygpath -w "$1"; else echo "$1"; fi; }
take() { until mkdir "$slot" 2>/dev/null; do sleep 30; done; }
give() { rmdir "$slot"; }

buildslot=${PROFILE_BUILD_SLOT:-$work/buildslot}

main=$(git -C "$root" grep -l -E '^func crashTold\(' -- '*.go')
[ "$(printf '%s' "$main" | grep -c .)" = 1 ] || { echo "symbol crashTold is in ${main:-no} Go file, not one, so tofu cannot be built" >&2; exit 2; }
mkdir -p "$work"
until mkdir "$buildslot" 2>/dev/null; do sleep 60; done
(cd "$root" && GOMAXPROCS=2 go build -p 2 -o "$work/profile.exe" ./bench/suites/perf/profile && GOMAXPROCS=2 go build -p 2 -o "$work/scale.exe" ./bench/suites/perf/scale &&
  { [ -n "${PROFILE_TOFU:-}" ] || GOMAXPROCS=2 go build -p 2 -o "$tofu" "./$(dirname "$main")"; })
built=$?
rmdir "$buildslot"
[ "$built" -eq 0 ] || exit 1
profile=$work/profile.exe

pprof() { if [ "$1" = profiled ]; then echo -pprof; fi; }

ask() {
  if [ "$1" -eq 0 ]; then
    echo "Read f0.txt in this directory and print its first line exactly. Do nothing else."
    return
  fi
  local files=""
  for k in $(seq 1 "$1"); do files="$files f$k.txt"; done
  echo "Use the spawn tool to start $1 sub-agents in parallel, all in your first reply, one per file:$files, all in this directory. Each sub-agent reads only its file and answers with that file's first line exactly. Do not read the files yourself. When every sub-agent has answered, print each first line on its own line."
}

project=$(native "$work/project")
"$work/scale.exe" fixture "$project"
for mode in stock profiled; do
  for agents in 0 1 10; do
    home=$(mktemp -d "$work/home-XXXX")
    "$work/scale.exe" cassette "$project" "$agents" "$(native "$work/cassette-$agents.jsonl")"
    "$profile" sample -label "scale-cassette-$mode-$agents" -dir "$project" -profile "$(native "$work/p-scale-cassette-$mode-$agents")" $(pprof "$mode") \
      -home "$(native "$home")" -cassette "$(native "$work/cassette-$agents.jsonl")" -credential none -wire cassette -- \
      "$tofu" run --dir "$project" --model claude-sub/claude-sonnet-5 --no-instructions --no-gate --sift free "spawn $agents"
    rm -rf "$home"
  done
done
[ "$live" = yes ] || exit 0
for mode in stock profiled; do
  for agents in 0 1 10; do
    take
    "$profile" sample -label "scale-live-$mode-$agents" -dir "$project" -profile "$(native "$work/p-scale-live-$mode-$agents")" $(pprof "$mode") -wire anthropic -- \
      "$tofu" run --dir "$project" --model claude-sub/claude-sonnet-5-5 --no-instructions --no-gate --sift free "$(ask "$agents")"
    give
  done
done
prompt=$(cat "$root/bench/suites/agent/rivals/testdata/prompt.txt")
for mode in stock profiled; do
  rivals=$work/rivals-$mode
  rm -rf "$rivals"
  "$profile" stage "$(native "$rivals/repo")" || exit 1
  take
  "$profile" sample -label "rivals-sonnet-$mode" -dir "$(native "$rivals/repo")" -profile "$(native "$work/p-rivals-$mode")" $(pprof "$mode") -go "$(native "$rivals/go")" -wire anthropic -- \
    "$tofu" run --dir "$(native "$rivals/repo")" --model claude-sub/claude-sonnet-5-5 --wire anthropic --effort medium --gate shadow --no-instructions "$prompt"
  give
done
