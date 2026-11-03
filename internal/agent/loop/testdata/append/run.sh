#!/bin/sh
set -eu
bin=$1
case=${2:-append}
here=$(cd "$(dirname "$0")" && pwd)
run=$(mktemp -d)
mkdir -p "$run/home" "$run/project"
printf 'import { test, expect } from "vitest";\n\ntest("adds", () => {\n  expect(1 + 1).toBe(2);\n});\n' > "$run/project/a.test.ts"
echo "home $run/home, made fresh in this run"
echo "case $case"
"$bin" version
"$bin" drive "$here/append.drive" --plain --dir "$run/project" --home "$run/home" --cassette "$here/$case.jsonl" --fresh > "$run/drive.out" 2>&1 || echo "drive exited $?"
python - "$run" <<'EOF'
import glob, json, sys
for path in glob.glob(sys.argv[1] + "/home/.tofu/projects/*/sessions/*/events.jsonl"):
    for line in open(path, encoding="utf-8"):
        event = json.loads(line)
        if event["kind"] == "tool_result":
            body = event["body"]
            print("--- tool_result", body.get("command", ""))
            print(body["content"])
EOF
echo "--- a.test.ts after the run"
cat -n "$run/project/a.test.ts"
