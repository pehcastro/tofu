#!/usr/bin/env bash
set -u
here=$(cd "$(dirname "$0")" && pwd)
bin=${MINIMAL_BIN:?set MINIMAL_BIN to the directory build.sh wrote}
work=${MINIMAL_WORK:?set MINIMAL_WORK to a scratch directory outside any repository}
slot=${MINIMAL_RIVAL_SLOT:?set MINIMAL_RIVAL_SLOT to the shared lock directory for live runs}
limits=${MINIMAL_LIMITS:-"256 128"}
rungs=${MINIMAL_RUNGS:-"0 1 4 10"}
arms=${MINIMAL_ARMS:-"tofu claude omp codex"}
results=${MINIMAL_RESULTS:-$here/results.jsonl}
export GOMAXPROCS=2

native() { cygpath -w "$1"; }

mkdir -p "$work"
minimal=$bin/minimal.exe
built="$("$bin/tofu.exe" version | awk '$1 == "version" { print $2 }') $(cat "$bin/commit")"
project=$work/project
rm -rf "$project"
"$bin/scale.exe" fixture "$project"
dir=$(native "$project")

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
  local arm=$1 memory=$2 agents=$3
  local common=(-agents "$agents" -memory "$memory" -cpu 1 -project "$dir" -out "$(native "$results")" -transcript "$(native "$work/$arm-$memory-$agents.txt")")
  case $arm in
    tofu)
      local home
      home=$(mktemp -d "$work/home-XXXX")
      "$bin/scale.exe" cassette "$dir" "$agents" "$work/cassette-$agents.jsonl"
      HOME=$(native "$home") USERPROFILE=$(native "$home") TOFU_DRIVE_CASSETTE=$(native "$work/cassette-$agents.jsonl") TOFU_MINIMAL_COUNT=$(native "$work/count.json") \
        "$minimal" sample -tool tofu -version "$built" -count "$(native "$work/count.json")" "${common[@]}" -- \
        "$(native "$bin/tofu.exe")" run --dir "$dir" --model claude-sub/claude-sonnet-5 --no-instructions --no-gate --sift free "spawn $agents"
      rm -rf "$home" "$work/count.json" ;;
    claude)
      "$minimal" sample -tool claude -version "$(claude --version)" -model claude-sonnet-5-5 -credential subscription -wire anthropic "${common[@]}" -- \
        "$(native "$(command -v claude)")" -p "$(ask "$agents" Agent)" --model sonnet --output-format json --setting-sources project --strict-mcp-config --allowedTools "Agent Task Read" ;;
    omp)
      "$minimal" sample -tool omp -version "$(omp --version)" -model claude-sonnet-5-5 -credential subscription -wire anthropic "${common[@]}" -- \
        "$(native "$(command -v omp)")" -p --model anthropic/claude-sonnet-5-5 --config "$(native "$here/../scale/omp-overlay.yml")" --no-extensions --no-skills --no-rules --no-session --no-title --auto-approve --mode json "$(ask "$agents" task)" ;;
    codex)
      "$minimal" sample -tool codex -version "$(codex --version)" -model gpt-5.6-sol -credential subscription -wire codex "${common[@]}" -- \
        "$(native "$(command -v codex)")" exec -m gpt-5.6-sol -c 'model="gpt-5.6-sol"' --skip-git-repo-check --sandbox read-only --json "$(ask "$agents" sub-agent)" ;;
  esac
}

for memory in $limits; do
  for arm in $arms; do
    for agents in $rungs; do
      if [ "$arm" = tofu ]; then
        line=$(measure "$arm" "$memory" "$agents")
      else
        until mkdir "$slot" 2>/dev/null; do sleep 30; done
        line=$(measure "$arm" "$memory" "$agents")
        rmdir "$slot"
      fi
      echo "$line"
      case $line in *"exit=0 "*) ;; *) echo "$arm stops at $agents agents under $memory MB"; break ;; esac
    done
  done
done
"$minimal" report "$(native "$results")"
