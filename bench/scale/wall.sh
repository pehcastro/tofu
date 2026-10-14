#!/usr/bin/env bash
set -u
here=$(cd "$(dirname "$0")" && pwd)
work=${SCALE_WORK:?set SCALE_WORK to a scratch directory outside any repository}
tofu=${SCALE_TOFU:?set SCALE_TOFU to the tofu binary measured}
slot=${SCALE_SLOT:-$work/slot}
rungs=${SCALE_RUNGS:-"2 4 10"}
arms=${SCALE_ARMS:-"cassette live"}
runs=${SCALE_RUNS:-3}
results=$here/wall.jsonl

native() { if command -v cygpath >/dev/null; then cygpath -w "$1"; else echo "$1"; fi; }

mkdir -p "$work"
(cd "$here/../.." && go build -o "$work/scale.exe" ./bench/scale) || exit 1
scale=$work/scale.exe

ask() {
  local files=""
  for k in $(seq 1 "$1"); do files="$files f$k.txt"; done
  echo "Use the spawn tool to start $1 sub-agents in parallel, all in your first reply, one per file:$files, all in this directory. Each sub-agent reads only its file and answers with that file's first line exactly. Do not read the files yourself. When every sub-agent has answered, print each first line on its own line."
}

for agents in $rungs; do
  for arm in $arms; do
    for run in $(seq 1 "$runs"); do
      project=$(mktemp -d "$work/project-XXXX")
      "$scale" fixture "$project"
      dir=$(native "$project")
      common=(-tofu "$(native "$tofu")" -arm "$arm" -agents "$agents" -run "$run" -project "$dir" -out "$(native "$results")" -transcript "$(native "$work/$arm-$agents-$run.txt")")
      case $arm in
        cassette)
          home=$(mktemp -d "$work/home-XXXX")
          "$scale" cassette "$dir" "$agents" "$work/cassette-$agents.jsonl"
          TOFU_DRIVE_CASSETTE=$(native "$work/cassette-$agents.jsonl") "$scale" wall "${common[@]}" -credential none -home "$(native "$home")" -- \
            run --dir "$dir" --model claude-sub/claude-sonnet-5 --no-instructions --no-gate --sift free "spawn $agents"
          rm -rf "$home" ;;
        live)
          until mkdir "$slot" 2>/dev/null; do sleep 30; done
          "$scale" wall "${common[@]}" -- \
            run --dir "$dir" --model claude-sub/claude-sonnet-5-5 --no-instructions --no-gate --sift free "$(ask "$agents")"
          rmdir "$slot" ;;
      esac
    done
  done
done
"$scale" wallreport "$(native "$results")" "$(native "$here/wall.md")" "$(native "$here/wall-notes.md")"
