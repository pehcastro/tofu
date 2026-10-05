#!/bin/sh
set -eu
[ $# -eq 2 ] || { echo "usage: sh bench/planspawn/run.sh <tofu.exe> <arm dir>"; exit 2; }
TOFU=$1; EXE=$(cygpath -w "$1"); OUT=$(realpath -m "$2")
ROOT=F:/localhost/ephem-sh/tofu
B=$ROOT/.playground/big
SEED=$B/seed
BASE=09e048fd61bc56df18676211f65703e7f0075f3a
QUIET=180
export GOMAXPROCS=4
mkdir -p "$OUT"
cd "$SEED"
git reset -q --hard $BASE && git clean -qfd

state() {
  list=$("$TOFU" session list --json)
  head=$(printf '%s\n' "$list" | sed -n 's/.*"head": "\(.*\)",/\1/p')
  agents=$(printf '%s\n' "$list" | grep -m1 '"sub_agents"' | tr -dc 0-9)
  outcome=$(printf '%s\n' "$list" | grep -m1 '"outcome"' | sed 's/.*: "\(.*\)",*/\1/')
  trace=$("$TOFU" session trace "$head" --json)
  events=$(printf '%s\n' "$trace" | grep -m1 '"events"' | tr -dc 0-9)
  running=$(printf '%s\n' "$trace" | grep -c '"status": "running"' || true)
  echo "$head $agents $outcome $events $running"
}

ask() {
  n=$1; task=$2; lead=$3; product=$4; mark=$5
  text="$lead $(tr '\n' ' ' < "$B/tasks/$task.txt" | tr -s ' ')When the sub-agent work is finished and reported, end your last reply with one line: the word FINISHED followed directly by the number $product, with no space."
  steps="$OUT/r$n.steps"
  printf 'wait 6s\ntype %s\nkey enter\nwaitfor FINISHED%s 2700s\nwait 20s\nshot r%s\nkey ctrl+c\nwait 8s\n' "$text" "$mark" "$n" > "$steps"
  args=""; [ "$n" -gt 1 ] && args="--continue"
  set -- $(state); before_head=$1; before_agents=${2:-0}
  powershell -NoProfile -ExecutionPolicy Bypass -File "$ROOT/.local/tools/tdrive.ps1" -Terminal conhost -Dir "$(cygpath -w "$SEED")" \
    -Steps "$(cygpath -w "$steps")" -Out "$(cygpath -w "$OUT")" -Program "$EXE" -Arguments "$args" > "$OUT/r$n.tdrive.txt" 2>&1 &
  driver=$!
  last=""; quiet=0
  while kill -0 $driver 2>/dev/null; do
    sleep 30
    now=$(state)
    set -- $now
    if [ "$now" = "$last" ] && [ "$3" = stopped ] && [ "$5" = 0 ] && { [ "$1" != "$before_head" ] || [ "${2:-0}" -gt "$before_agents" ]; }; then
      quiet=$((quiet + 30))
    else
      quiet=0
    fi
    last=$now
    if [ $quiet -ge $QUIET ]; then
      echo "stopped by the bench after the session sat finished for ${QUIET}s: $now" >> "$OUT/r$n.tdrive.txt"
      taskkill //PID "$(cat /proc/$driver/winpid)" //T //F >> "$OUT/r$n.tdrive.txt" 2>&1 || true
      break
    fi
  done
  wait $driver || true
  set -- $(state)
  "$TOFU" session trace "$1" --json > "$OUT/r$n.trace.json"
  git status --porcelain -uall > "$OUT/r$n.status"
  sh "$B/grade/check.sh" "$task" "$SEED" > "$OUT/r$n.check" 2>&1 || true
}

one="Give this to one sub-agent."
two="Split this between two sub-agents that work at the same time: one implements recursive_depth and max_entries in the list_directory tool with its tests, the other declares both parameters in the tool's schema for every model family and updates the schema snapshot."
ask 1 feature-small "$one" "7 times 3" 21
ask 2 medium-glob "$one" "8 times 4" 32
ask 3 hard-duration "$one" "9 times 5" 45
ask 4 hard-ls "$two" "6 times 9" 54
git add -A -N && git diff > "$OUT/final.patch"
git reset -q --hard $BASE && git clean -qfd
git status --short > "$OUT/seed-after.status"
