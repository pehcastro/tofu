#!/usr/bin/env bash
set -u
here=$(cd "$(dirname "$0")" && pwd)
root=$(git -C "$here" rev-parse --show-toplevel)
work=${MINIMAL_WORK:?set MINIMAL_WORK to a scratch directory outside any repository}
slot=${MINIMAL_BUILD_SLOT:?set MINIMAL_BUILD_SLOT to the shared build lock directory}

native() { if command -v cygpath >/dev/null; then cygpath -w "$1"; else echo "$1"; fi; }
holder() {
  local found
  found=$(git -C "$root" grep -l -E "$1" HEAD -- '*.go' "${@:2}" | sed 's/^HEAD://')
  if [ "$(printf '%s' "$found" | grep -c .)" != 1 ]; then
    echo "symbol $1 is in ${found:-no} Go file at HEAD, not one, so the minimal build cannot be made" >&2
    return 2
  fi
  echo "$found"
}

overlay=$(git -C "$root" ls-files --full-name "$here/overlay/profile.go")
profiled=$(holder '^func StartProfile\(' ":(exclude)$overlay") || exit 2
main=$(holder '^func crashTold\(') || exit 2
product=./$(dirname "$main")
src=$work/src
rm -rf "$src"
mkdir -p "$src"
git -C "$root" rev-parse --short=8 HEAD > "$work/commit"
git -C "$root" archive HEAD -- . ':(exclude)apps/desk' | tar -x -C "$src"
printf '{"Replace":{"%s":"%s"}}' "$(native "$src/$profiled" | sed 's/\\/\\\\/g')" "$(native "$here/overlay/profile.go" | sed 's/\\/\\\\/g')" > "$work/overlay.json"
counted=(-tags minimal -overlay "$(native "$work/overlay.json")")
until mkdir "$slot" 2>/dev/null; do sleep 60; done
export GOMAXPROCS=2
(cd "$src" &&
  GOOS=linux go build -p 2 -buildvcs=false -trimpath "${counted[@]}" -o "$work/tofu-linux" "$product" &&
  GOOS=linux go build -p 2 -buildvcs=false -trimpath -o "$work/tofu-linux-head" "$product" &&
  go build -p 2 -buildvcs=false -trimpath "${counted[@]}" -o "$work/tofu.exe" "$product" &&
  GOOS=linux go build -p 2 -o "$work/scale-linux" ./bench/suites/perf/scale &&
  go build -p 2 -o "$work/scale.exe" ./bench/suites/perf/scale) &&
  (cd "$root" && GOOS=linux go build -p 2 -o "$work/minimal-linux" ./bench/suites/perf/minimal && go build -p 2 -o "$work/minimal.exe" ./bench/suites/perf/minimal)
built=$?
rmdir "$slot"
exit "$built"
