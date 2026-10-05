#!/bin/sh
set -eu
bin=$1
run=$2
filler=${3:-40}
here=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$run/home" "$run/project"
: > "$run/server.log"
python -u "$here/serve.py" "$run/server.log" 2000 "$filler" > "$run/port" &
server=$!
trap 'kill $server' EXIT
until [ -s "$run/port" ]; do :; done
port=$(cat "$run/port")
echo "home $run/home, made fresh in this run"
echo "server 127.0.0.1:$port, a 2000-line page, each line padded with $filler dots"
"$bin" version
sed -e "s/PORT/$port/g" "$here/fetch.jsonl.in" > "$run/fetch.jsonl"
"$bin" drive "$here/fetch.drive" --plain --dir "$run/project" --home "$run/home" --cassette "$run/fetch.jsonl" --fresh > "$run/drive.out" 2>&1 || echo "drive exited $?"
python - "$run" <<'EOF'
import glob, json, sys
for path in glob.glob(sys.argv[1] + "/home/.tofu/projects/*/sessions/*/events.jsonl"):
    for line in open(path, encoding="utf-8"):
        event = json.loads(line)
        if event["kind"] == "tool_result":
            body = event["body"]
            content = body["content"]
            page = content.split(" begins>>>\n", 1)[-1].split("\n<<<", 1)[0].split("\n")
            print("--- fetch", body.get("args", ""), "result_bytes", body["result_bytes"], "rendered_bytes", body["rendered_bytes"], "handle", body.get("result_handle", ""))
            print(content.split("\n", 1)[0])
            print("page lines", len(page), "first", repr(page[0][:20]), "last", repr(page[-1][:20]))
EOF
echo "--- server log, $(wc -l < "$run/server.log") request(s)"
cat "$run/server.log"
