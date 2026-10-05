#!/bin/sh
# Usage: curl -fsSL https://raw.githubusercontent.com/pehcastro/tofu/release/scripts/install.sh | sh
# Pin a version with TOFU_VERSION=0.5.0, change the place with TOFU_INSTALL_DIR, read assets from TOFU_RELEASE_URL.
set -eu

fail() {
    echo "tofu install: $*" >&2
    exit 1
}

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) fail "unsupported OS $(uname -s), use install.ps1 on Windows" ;;
esac

case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) fail "unsupported architecture $(uname -m)" ;;
esac

if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
    arch=arm64
fi

if [ -n "${TOFU_RELEASE_URL:-}" ]; then
    base="${TOFU_RELEASE_URL%/}"
elif [ -n "${TOFU_VERSION:-}" ]; then
    base="https://github.com/pehcastro/tofu/releases/download/v${TOFU_VERSION#v}"
else
    base="https://github.com/pehcastro/tofu/releases/latest/download"
fi
dir="${TOFU_INSTALL_DIR:-$HOME/.local/bin}"

if command -v curl >/dev/null 2>&1; then
    fetch() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
    fetch() { wget -q -O "$2" "$1"; }
else
    fail "needs curl or wget"
fi

if command -v sha256sum >/dev/null 2>&1; then
    sum() { sha256sum "$1" | cut -d ' ' -f 1; }
else
    sum() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

fetch "$base/checksums.txt" "$tmp/checksums.txt" || fail "cannot download $base/checksums.txt"
line="$(grep "  tofu_.*_${os}_${arch}\.tar\.gz\$" "$tmp/checksums.txt" | head -n 1)" || true
[ -n "$line" ] || fail "no ${os}_${arch} asset in $base/checksums.txt"
want="${line%% *}"
asset="${line##* }"
if [ -n "${TOFU_VERSION:-}" ] && [ "$asset" != "tofu_${TOFU_VERSION#v}_${os}_${arch}.tar.gz" ]; then
    fail "$base has $asset, not version ${TOFU_VERSION#v}"
fi

echo "downloading $asset"
fetch "$base/$asset" "$tmp/$asset" || fail "cannot download $base/$asset"
got="$(sum "$tmp/$asset")"
[ "$got" = "$want" ] || fail "checksum mismatch for $asset: want $want, got $got"

tar -xzf "$tmp/$asset" -C "$tmp" tofu
mkdir -p "$dir"
chmod 755 "$tmp/tofu"
mv -f "$tmp/tofu" "$dir/tofu"
echo "installed $dir/tofu"
"$dir/tofu" version

case ":$PATH:" in
    *":$dir:"*) ;;
    *)
        echo "$dir is not on PATH. Add this line to your shell profile (~/.bashrc, ~/.zshrc):"
        echo "  export PATH=\"$dir:\$PATH\""
        ;;
esac
