#!/bin/sh
set -eu
bin=$1
here=$(cd "$(dirname "$0")" && pwd)
run=$(mktemp -d)
mkdir -p "$run/home" "$run/project"
awk 'BEGIN{for(i=1;i<=1600;i++){s=sprintf("line %05d of 1600 ",i); while(length(s)<63) s=s "."; print s}}' > "$run/project/big.txt"
echo "home $run/home, made fresh in this run"
echo "big.txt $(wc -c < "$run/project/big.txt") bytes"
"$bin" version
"$bin" drive "$here/read.drive" --plain --dir "$run/project" --home "$run/home" --cassette "$here/read.jsonl" --fresh > "$run/read.out" 2>&1
stored=$(find "$run/home" -path '*artifacts*' -name '*.bin')
handle=$(basename "$stored" .bin)
echo "artifact $handle holds $(wc -c < "$stored") bytes"
sed "s/HANDLE/$handle/g" "$here/fetch.jsonl.in" > "$run/fetch.jsonl"
"$bin" drive "$here/fetch.drive" --plain --dir "$run/project" --home "$run/home" --cassette "$run/fetch.jsonl" --continue > "$run/fetch.out" 2>&1
python - "$run" <<'EOF'
import glob, json, sys
for path in glob.glob(sys.argv[1] + "/home/.tofu/projects/*/sessions/*/events.jsonl"):
    for line in open(path, encoding="utf-8"):
        event = json.loads(line)
        if event["kind"] == "tool_result":
            body = event["body"]
            print("---", body.get("command", ""), "result_bytes", body["result_bytes"], "rendered_bytes", body["rendered_bytes"])
            print(body["content"])
EOF
