#!/usr/bin/env bash
set -u
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
work=${PROFILE_WORK:?set PROFILE_WORK to a scratch directory outside any repository}
label=${PROFILE_LABEL:-head}
buildslot=${PROFILE_BUILD_SLOT:-$work/buildslot}

native() { if command -v cygpath >/dev/null; then cygpath -w "$1"; else echo "$1"; fi; }

commit=$(git -C "$root" rev-parse --short HEAD)
src=$work/src-$label
rm -rf "$src"
mkdir -p "$src" "$work/idle"
git -C "$root" archive HEAD go.mod go.sum bench cmd internal interface library third_party | tar -x -C "$src"
until mkdir "$buildslot" 2>/dev/null; do sleep 60; done
(cd "$root" && GOMAXPROCS=2 go build -p 2 -o "$work/profile.exe" ./bench/profile) &&
  (cd "$src" && GOMAXPROCS=2 go build -p 2 -buildvcs=false -trimpath -ldflags="-s -w" -o "$work/$label-strip.exe" ./cmd/tofu &&
    GOMAXPROCS=2 go build -p 2 -buildvcs=false -trimpath -o "$work/$label-full.exe" ./cmd/tofu)
built=$?
rmdir "$buildslot"
[ "$built" -eq 0 ] || exit 1

profile=$work/profile.exe
tofu=$(native "$work/$label-strip.exe")
"$profile" size -label "$label" -build "$commit, git archive HEAD" -stripped "$tofu" -full "$(native "$work/$label-full.exe")"
"$profile" idle -label "$label-app" -build "$commit stripped" -work "$(native "$work/idle")" -- "$tofu"
TOFU_MODELS_REGISTRY_URL=http://127.0.0.1:9/api.json "$profile" idle -label "$label-app-noreload" -build "$commit stripped, catalog reload refused" -work "$(native "$work/idle")" -- "$tofu"
"$profile" idle -label "$label-serve" -build "$commit stripped" -work "$(native "$work/idle")" -- "$tofu" serve --stdio
