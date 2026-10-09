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

tile_rule="every screen is one tile: shell header strip, one inner_card (memory m20)"
screen_roots=(crates/desk/src/screens/*/mod.rs crates/desk/src/modules/editor/mod.rs)
calls() {
    grep -HnE "(^|[^_a-zA-Z0-9])$1\\(" "${@:2}" | grep -vE ':[0-9]+:[[:space:]]*(pub(\([a-z]+\))? )?fn '
}

tile_breaks() {
    calls outer_card -r --include='*.rs' "${roots[@]}" | awk -F: '{ print $1 ":outer_card\t" $2 }'
    for file in "${screen_roots[@]}"; do
        if ! calls '(shell|frame::tile|frame::window)' "$file" >/dev/null &&
            ! calls '(tile|window)' "$file" | grep -vE '(::|\.)(tile|window)\(' >/dev/null; then
            printf '%s:shell\t%s\n' "$file" "$(grep -nm1 'impl Render' "$file" | cut -d: -f1 | grep . || echo 1)"
        fi
    done
    calls inner_card -r --include='*.rs' "${roots[@]}" | awk -F: '
        { lines[$1] = lines[$1] (count[$1]++ ? "," : "") $2 }
        END { for (file in count) if (count[file] > 1) print file ":inner_card\t" lines[file] }'
}

tiles=$(tile_breaks | sort)
hits=$( { wide_consts; placed_offsets; printf '%s\n' "$tiles" | cut -f1; } | sed '/^$/d' | sort -u)

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
untiled=$(printf '%s\n' "$broken" | grep -E ':(outer_card|shell|inner_card)$' || true)
broken=$(printf '%s\n' "$broken" | grep -vE ':(outer_card|shell|inner_card)$' || true)
if [ -n "$untiled" ]; then
    printf '%s\n' "$tiles" | grep -F -f <(printf '%s\n' "$untiled" | sed 's/$/\t/') |
        while IFS=$'\t' read -r key lines; do
            file=${key%:*}
            kind=${key##*:}
            case "$kind" in
                outer_card) said="calls outer_card(, a second frame" ;;
                shell) said="draws no shell(, frame::tile( or frame::window(" ;;
                inner_card) said="calls inner_card( more than once" ;;
            esac
            echo "screen-fit: $file:$lines: $said: $tile_rule"
        done
    echo "fix it, or add '<path>:<outer_card|shell|inner_card>  reason' to $allow when it frames one of several side-by-side sections"
    failed=1
fi
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
