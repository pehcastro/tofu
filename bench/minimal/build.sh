#!/usr/bin/env bash
set -u
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
work=${MINIMAL_WORK:?set MINIMAL_WORK to a scratch directory outside any repository}
slot=${MINIMAL_BUILD_SLOT:?set MINIMAL_BUILD_SLOT to the shared build lock directory}

native() { if command -v cygpath >/dev/null; then cygpath -w "$1"; else echo "$1"; fi; }

src=$work/src
rm -rf "$src"
mkdir -p "$src"
git -C "$root" rev-parse --short=8 HEAD > "$work/commit"
git -C "$root" archive HEAD go.mod go.sum bench cmd internal interface library third_party | tar -x -C "$src"
printf '{"Replace":{"%s":"%s"}}' "$(native "$src/internal/sys/profile.go" | sed 's/\\/\\\\/g')" "$(native "$here/overlay/profile.go" | sed 's/\\/\\\\/g')" > "$work/overlay.json"
counted=(-tags minimal -overlay "$(native "$work/overlay.json")")
until mkdir "$slot" 2>/dev/null; do sleep 60; done
export GOMAXPROCS=2
(cd "$src" &&
  GOOS=linux go build -p 2 -buildvcs=false -trimpath "${counted[@]}" -o "$work/tofu-linux" ./cmd/tofu &&
  GOOS=linux go build -p 2 -buildvcs=false -trimpath -o "$work/tofu-linux-head" ./cmd/tofu &&
  go build -p 2 -buildvcs=false -trimpath "${counted[@]}" -o "$work/tofu.exe" ./cmd/tofu &&
  GOOS=linux go build -p 2 -o "$work/scale-linux" ./bench/scale &&
  go build -p 2 -o "$work/scale.exe" ./bench/scale) &&
  (cd "$root" && GOOS=linux go build -p 2 -o "$work/minimal-linux" ./bench/minimal && go build -p 2 -o "$work/minimal.exe" ./bench/minimal)
built=$?
rmdir "$slot"
exit "$built"
