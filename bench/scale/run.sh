#!/usr/bin/env bash
set -u
here=$(cd "$(dirname "$0")" && pwd)
work=${SCALE_WORK:?set SCALE_WORK to a scratch directory outside any repository}
slot=${SCALE_SLOT:-$work/slot}
rungs=${SCALE_RUNGS:-"0 1 2 4 6 8 10"}
arms=${SCALE_ARMS:-"tofu-cassette tofu claude omp codex"}
results=$here/results.jsonl

native() { if command -v cygpath >/dev/null; then cygpath -w "$1"; else echo "$1"; fi; }

mkdir -p "$work"
(cd "$here/../.." && go build -o "$work/scale.exe" ./bench/scale) || exit 1
scale=$work/scale.exe
project=$work/project
"$scale" fixture "$project"
dir=$(native "$project")
built=$(tofu version | awk '$1 == "version" || $1 == "commit" { printf "%s ", $2 }')

take() { until mkdir "$slot" 2>/dev/null; do sleep 30; done; }
give() { rmdir "$slot"; }

ask() {
  local agents=$1 tool=$2
  if [ "$agents" -eq 0 ]; then
    echo "Read f0.txt in this directory and print its first line exactly. Do nothing else."
    return
  fi
  local files=""
  for k in $(seq 1 "$agents"); do files="$files f$k.txt"; done
  echo "Use the $tool tool to start $agents sub-agents in parallel, all in your first reply, one per file:$files, all in this directory. Each sub-agent reads only its file and answers with that file's first line exactly. Do not read the files yourself. When every sub-agent has answered, print each first line on its own line."
}

measure() {
  local arm=$1 agents=$2
  local transcript
  transcript=$(native "$work/$arm-$agents.txt")
  local common=(-agents "$agents" -project "$dir" -out "$(native "$results")" -transcript "$transcript")
  case $arm in
    tofu-cassette)
      local home
      home=$(mktemp -d "$work/home-XXXX")
      "$scale" cassette "$dir" "$agents" "$work/cassette-$agents.jsonl"
      HOME=$(native "$home") USERPROFILE=$(native "$home") TOFU_DRIVE_CASSETTE=$(native "$work/cassette-$agents.jsonl") \
        "$scale" sample -tool tofu -version "$built" -arm cassette -credential none -wire cassette -model cassette "${common[@]}" -- \
        tofu run --dir "$dir" --model claude-sub/claude-sonnet-5 --no-instructions --no-gate --sift free "spawn $agents"
      rm -rf "$home" ;;
    tofu)
      "$scale" sample -tool tofu -version "$built" -wire anthropic -model-from ' model (\S+) asked_as' "${common[@]}" -- \
        tofu run --dir "$dir" --model claude-sub/claude-sonnet-5-5 --no-instructions --no-gate --sift free "$(ask "$agents" spawn)" ;;
    claude)
      "$scale" sample -tool claude -wire anthropic -model-from '"canonicalModel":"([^"]+)"' "${common[@]}" -- \
        claude -p "$(ask "$agents" Agent)" --model sonnet --output-format json --setting-sources project --strict-mcp-config --allowedTools "Agent Task Read" ;;
    omp)
      "$scale" sample -tool omp -wire anthropic -model-from '"model":"([^"]+)"' "${common[@]}" -- \
        omp -p --model anthropic/claude-sonnet-5-5 --config "$(native "$here/omp-overlay.yml")" --no-extensions --no-skills --no-rules --no-session --no-title --auto-approve --mode json "$(ask "$agents" task)" ;;
    codex)
      "$scale" sample -tool codex -wire codex -model gpt-5.6-sol "${common[@]}" -- \
        codex exec -m gpt-5.6-sol -c 'model="gpt-5.6-sol"' --skip-git-repo-check --sandbox read-only --json "$(ask "$agents" sub-agent)" ;;
  esac
}

take
"$scale" check "$(native "$results")"
give
stopped=""
for agents in $rungs; do
  for arm in $arms; do
    case " $stopped " in *" $arm "*) continue ;; esac
    take
    line=$(measure "$arm" "$agents")
    give
    echo "$line"
    want=$(( agents > 0 ? agents : 1 ))
    case $line in *"answered=$want exit=0"*) ;; *) stopped="$stopped $arm"; echo "$arm stops at $agents agents" ;; esac
  done
done
"$scale" report "$(native "$results")" "$(native "$here/report.md")"
