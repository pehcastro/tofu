#!/usr/bin/env bash
set -u
cd "$(dirname "$0")/.."

roots=(crates/desk/src/screens crates/desk/src/modules)
allow=scripts/screen-fit.allow
most=400
window=12

wide_consts() {
    grep -rnE --include='*.rs' 'const [A-Z0-9_]+: [^=]*f32[^=]* =' "${roots[@]}" |
        awk -v most="$most" '{
            match($0, /const [A-Z0-9_]+:/)
            name = substr($0, RSTART + 6, RLENGTH - 7)
            split($0, at, ":")
            rest = substr($0, index($0, "=") + 1)
            gsub(/0x[0-9a-fA-F_]+/, "", rest)
            while (match(rest, /-?[0-9]+(\.[0-9]+)?/)) {
                value = substr(rest, RSTART, RLENGTH) + 0
                if (value > most || -value > most) { print at[1] ":" name; break }
                rest = substr(rest, RSTART + RLENGTH)
            }
        }'
}

placed_offsets() {
    grep -rlE --include='*.rs' '\.absolute\(\)' "${roots[@]}" | while read -r file; do
        awk -v file="$file" -v window="$window" '
            function offsets(text) {
                while (match(text, /\.(left|top|inset)\(-?px\([^)]*\)+/)) {
                    call = substr(text, RSTART + 1, RLENGTH - 1)
                    gsub(/ /, "", call)
                    print file ":" call
                    text = substr(text, RSTART + RLENGTH)
                }
            }
            /\.absolute\(\)/ {
                left = window
                offsets(substr($0, index($0, ".absolute()")))
                next
            }
            left > 0 {
                if ($0 ~ /(\.child|\.children|div\(\)|;)/) { left = 0; next }
                offsets($0)
                left--
            }
        ' "$file"
    done
}

hits=$( { wide_consts; placed_offsets; } | sort -u)

allowed=$(grep -vE '^[[:space:]]*(#|$)' "$allow" 2>/dev/null || true)
reasonless=$(printf '%s\n' "$allowed" | awk 'NF && NF < 2')
keys=$(printf '%s\n' "$allowed" | awk 'NF { print $1 }' | sort -u)

failed=0
if [ -n "$reasonless" ]; then
    echo "screen-fit: an allow line needs a reason after the key:"
    printf '%s\n' "$reasonless" | sed 's/^/  /'
    failed=1
fi

broken=$(comm -23 <(printf '%s\n' "$hits" | sed '/^$/d') <(printf '%s\n' "$keys" | sed '/^$/d'))
if [ -n "$broken" ]; then
    echo "screen-fit: a screen carries board coordinates (an absolute left or top in px, or an f32 const over $most):"
    printf '%s\n' "$broken" | sed 's/^/  /'
    echo "lay it out with flex in the area it is given, or add 'path:key  reason' to $allow"
    failed=1
fi

stale=$(comm -13 <(printf '%s\n' "$hits" | sed '/^$/d') <(printf '%s\n' "$keys" | sed '/^$/d'))
if [ -n "$stale" ]; then
    echo "screen-fit: an allow line matches nothing in the tree, delete it:"
    printf '%s\n' "$stale" | sed 's/^/  /'
    failed=1
fi

[ "$failed" = 0 ] && echo "screen-fit: $(printf '%s\n' "$hits" | sed '/^$/d' | wc -l | tr -d ' ') allowed, nothing else"
exit "$failed"
