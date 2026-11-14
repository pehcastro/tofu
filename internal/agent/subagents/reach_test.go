package subagents

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func lay(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.ToSlash(dir)
}

func TestAChecksReachIsJudgedByTheFoldersItReachesNotByItsDot(t *testing.T) {
	lay(t, "go.mod", "cart.go", "page.go", "money.go", ".git/hooks/x.go", "testdata/seed.go", "_old/x.go", "notes/readme.md")
	owner := NewBoundary("go-dev-1", "", []string{"cart.go", "page.go", "export.go", "ANSWER.txt"})
	drive(t, owner, POSIX, []bashRow{
		{"gofmt -l .", ""},
		{"gofmt -l cart.go page.go", ""},
		{"gofmt -d -s cart.go", ""},
		{"gofmt -l . && go vet . && go test .", ""},
		{"go test ./...", ""},
		{"go vet ./... && go test ./...", ""},
		{"go test all", treeWide},
		{"gofmt -w cart.go", ""},
		{"gofmt -w money.go", notOwned},
		{"gofmt -l -w .", notOwned},
		{"gofmt -cpuprofile prof.out cart.go", notRead},
		{"cd notes && gofmt -l .", ""},
	})
	drive(t, NewBoundary("claim", "", nil), POSIX, []bashRow{{"gofmt -l .", treeWide}, {"go test ./...", treeWide}})
	drive(t, NewBoundary("md", "", []string{"notes/**"}), POSIX, []bashRow{{"gofmt -l .", treeWide}})
}

func TestACheckOverAnotherAgentsFolderIsStillTreeWide(t *testing.T) {
	lay(t, "go.mod", "internal/mine/a.go", "internal/theirs/b.go", "cmd/app/main.go")
	owner := NewBoundary("go-dev-1", "", []string{"internal/mine/**"})
	drive(t, owner, POSIX, []bashRow{
		{"gofmt -l .", treeWide},
		{"go test ./...", treeWide},
		{"golangci-lint run", treeWide},
		{"cd internal/mine && gofmt -l .", treeWide},
		{"gofmt -l internal/mine", ""},
		{"go test ./internal/mine/...", ""},
		{"gofmt -w internal/theirs/b.go", notOwned},
		{"gofmt -w internal/mine/a.go", ""},
		{"cd internal/mine && gofmt -w a.go", ""},
	})
	drive(t, NewBoundary("all", "", []string{"**"}), POSIX, []bashRow{{"go test ./...", ""}, {"gofmt -l .", ""}, {"go vet all", treeWide}})
}

func TestTheShellsOwnStatusAndTheRtkReadersAreReads(t *testing.T) {
	root := lay(t, "go.mod", "src/a.rs", "src/b.go")
	scratch := root + "/.tofu/scratch/rust-dev-1"
	owner := NewBoundary("rust-dev-1", scratch, []string{"src/**", "target/**"})
	drive(t, owner, POSIX, []bashRow{
		{"cargo test 2>&1 | tail -5; echo exit ${PIPESTATUS[0]}", ""},
		{"go test ./src/; echo $?", ""},
		{"echo ${PIPESTATUS[$(rm x)]}", notRead},
		{"rtk read src/a.rs", ""},
		{"rtk grep -n fn src", ""},
		{"rtk run 'rm x'", notRead},
		{"rustfmt src/a.rs", ""},
		{"rustfmt --check README.rs", ""},
		{"rustfmt lib.rs", notOwned},
	})
	drive(t, owner, PowerShell, []bashRow{
		{"cargo test 2>&1 | tail -5; echo ${PIPESTATUS[0]}", ""},
		{"echo ${c:/README.md}", notRead},
		{"echo ${PIPESTATUS[0]} | ForEach-Object { rm x }", notRead},
	})
}

func TestAScriptInAHeldFolderRunsAndATimeoutIsJudgedByWhatItWraps(t *testing.T) {
	root := lay(t, "Cargo.toml", "src/main.rs", "shots/d1/run.ps1", "shots/other/run.ps1", "run.ps1")
	scratch := root + "/.tofu/scratch/rust-dev-7"
	owner := NewBoundary("rust-dev-7", scratch, []string{"src/**", "shots/d1/**", "run.ps1"})
	const askGrant = "ask the orchestrator to grant"
	rows := []bashRow{
		{"powershell -NoProfile -File shots/d1/run.ps1 -Out shots/d1/a.png", ""},
		{"pwsh -NoProfile -NonInteractive -ExecutionPolicy Bypass -File shots/d1/run.ps1", ""},
		{"powershell.exe -file shots/d1/run.ps1", ""},
		{"powershell -NoProfile -File shots/other/run.ps1", askGrant},
		{"cd shots/other && powershell -File run.ps1", askGrant},
		{"powershell -File " + scratch + "/tmp/x.ps1", notRead},
		{"powershell -File ../run.ps1", notRead},
		{"powershell -File", notRead},
		{`powershell -Command "Remove-Item C:/x"`, notRead},
		{"powershell -NoProfile -Command shots/d1/run.ps1", notRead},
		{"powershell -NoProfile -EncodedCommand AAAA", notRead},
		{"timeout 60 cargo test", ""},
		{"timeout -k 5 --signal=KILL 60 cargo build --target-dir target_agent_7", "shared target"},
		{"timeout 600 powershell -NoProfile -File shots/d1/run.ps1", ""},
		{"rtk proxy timeout 60 powershell -File shots/d1/run.ps1", ""},
		{"timeout 60 cargo build --target-dir target", "shared target"},
		{"timeout 60 powershell -File shots/other/run.ps1", askGrant},
		{`timeout 60 powershell -Command "Remove-Item C:/x"`, notRead},
		{"timeout 5 curl http://x", notRead},
		{"timeout 5", notRead},
		{"timeout 5 timeout 5 gofmt -w README.go", notOwned},
	}
	drive(t, owner, POSIX, rows)
	drive(t, owner, PowerShell, rows)
}

func TestAnAgentReadsTheRulesBuildsWhatItHoldsAndSharesOneTarget(t *testing.T) {
	root := lay(t, ".tofu/rules/desk_widen_primitives@1.yaml", ".tofu/settings.json", ".tofu/hooks.json",
		"apps/desk/crates/desk/src/main.rs", "apps/desk/crates/desk/src/frame.rs", "apps/desk/crates/desk/examples/frame.rs",
		"apps/desk/crates/perf/src/lib.rs", "apps/desk/crates/other/src/lib.rs", "apps/desk/crates/other/src/bin/tool.rs",
		"apps/desk/target/CACHEDIR.TAG", "apps/desk/target/package/stale/Cargo.toml")
	for file, body := range map[string]string{
		"apps/desk/Cargo.toml":                      "[workspace]\nmembers = [\"crates/*\"]\n",
		"apps/desk/crates/desk/Cargo.toml":          "[package]\nname = \"desk\"\n",
		"apps/desk/crates/perf/Cargo.toml":          "[package]\nname = \"perf\"\n",
		"apps/desk/crates/other/Cargo.toml":         "[package]\nname = \"other\"\n",
		"apps/desk/target/package/stale/Cargo.toml": "[package]\nname = \"desk\"\n",
	} {
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scratchpad := root + "/home/.tofu/projects/p/scratchpad"
	scratch, target := scratchpad+"/sessions/s/agents/c1", scratchpad+"/cache/cargo"
	owner := NewBoundary("c1", scratch, []string{"apps/desk/crates/perf/**", "apps/desk/crates/desk/src/frame.rs", "apps/desk/crates/core/schema/**"})
	const (
		tofuState   = "is tofu's own state"
		holdNothing = "holds nothing in"
		noCrate     = "names no crate"
		shared      = "shared target"
	)
	drive(t, owner, POSIX, []bashRow{
		{"cat .tofu/rules/desk_widen_primitives@1.yaml", ""},
		{"ls .tofu/rules && head -3 .tofu/rules/desk_widen_primitives@1.yaml", ""},
		{"git log --oneline .tofu/rules", ""},
		{"cat .tofu/settings.json", tofuState},
		{"cat .tofu/rules/../settings.json", tofuState},
		{"cat < .tofu/hooks.json", tofuState},
		{"cd .tofu && cat settings.json", tofuState},
		{"cd .tofu/rules && cat ../settings.json", tofuState},
		{"grep -r key .tofu", tofuState},
		{"ls .tofu", tofuState},
		{"cat .tofu/*", tofuState},
		{"git show HEAD:.tofu/settings.json", tofuState},
		{"cat ~/.tofu/accounts.json", tofuState},
		{"rg --files apps/desk/.tofu/x", tofuState},
		{"cat $TMPDIR/run.log", ""},
		{"tofu serve --schema", ""},
		{"tofu serve --schema > apps/desk/crates/core/schema/tofu.host.json", ""},
		{"tofu serve --schema > apps/desk/crates/desk/schema.json", notOwned},
		{"tofu serve", notRead},
		{"tofu serve --stdio", notRead},
		{"tofu serve --schema --stdio", notRead},
		{"tofu boardy move TOFU-1 done", notRead},
		{"tofu drive desk", ""},
		{"tofu drive desk --screen work click:20,20 wait:500ms type:hello key:enter shot:after", ""},
		{"tofu drive desk --out $TMPDIR/shots click:20,20", ""},
		{"tofu drive desk --out=$TMPDIR/shots click:20,20", ""},
		{"rtk proxy tofu drive desk click:20,20 && cat $TMPDIR/shots/steps.log", ""},
		{"timeout 120 tofu drive desk click:20,20", ""},
		{"tofu drive desk --out apps/desk/crates/perf/shots", ""},
		{"tofu drive desk --out apps/desk/crates/other/shots", notOwned},
		{"tofu drive desk --out " + root + "/elsewhere", notOwned},
		{"tofu drive desk --out $SHOTS", unreadable},
		{"tofu drive desk --exe apps/desk/target/debug/desk.exe", notRead},
		{"tofu drive desk --exe=x.exe", notRead},
		{"tofu drive steps.drive", notRead},
		{"tofu drive", notRead},
		{"tofu drive --home x desk", notRead},
		{"cargo build -p desk", ""},
		{"cargo build -p perf", ""},
		{"cargo build --package=desk && cargo build -pperf", ""},
		{"cargo build --package desk --release", ""},
		{"cargo build -p other", holdNothing},
		{"cargo build -p desk -p other", holdNothing},
		{"cargo build -p nosuch", noCrate},
		{"cargo build -p $CRATE", "is not a path this check can read"},
		{"cargo build", noCrate},
		{"cd apps/desk && cargo build", "no crate of its own"},
		{"cd apps/desk && cargo build -p desk --workspace", "every crate"},
		{"cargo build --all", "every crate"},
		{"cd apps/desk/crates/perf && cargo build", ""},
		{"cd apps/desk/crates/perf/src && cargo build", ""},
		{"cd apps/desk/crates/other && cargo build", holdNothing},
		{"cargo build --manifest-path apps/desk/crates/perf/Cargo.toml", ""},
		{"cargo build --manifest-path=apps/desk/crates/other/Cargo.toml", holdNothing},
		{"cargo run -p desk --example frame", ""},
		{"cargo run -p desk -- --out $TMPDIR/a.png", ""},
		{"cargo run -p desk -- --out /elsewhere/a.png", ""},
		{"cargo run -p other", holdNothing},
		{"timeout 300 cargo run -p desk --example frame && rtk proxy cargo build -p perf", ""},
		{"cargo build -p desk --target-dir target_agent_7", shared},
		{"cargo test -p perf --target-dir=out", shared},
		{"CARGO_TARGET_DIR=" + scratch + "/target cargo build -p desk", shared},
		{"CARGO_TARGET_DIR=$TMPDIR/target cargo test", shared},
		{"cargo test -p other", ""},
		{"echo $CARGO_TARGET_DIR", ""},
		{"$CARGO_TARGET_DIR/debug/desk.exe --headless", ""},
		{target + "/debug/examples/frame.exe --frames 60 > $TMPDIR/frames.log", ""},
		{target + "/release/perf", ""},
		{target + "/debug/tool.exe", holdNothing},
		{target + "/debug/nosuch.exe", noCrate},
		{target + "/debug/deps/desk-0a1b2c.exe", noCrate},
		{target + "/../x.exe", "is not a bare program name"},
		{"apps/desk/target/debug/desk.exe", ""},
		{"$CARGO_TARGET_DIR/debug/desk.exe $(rm -rf src)", "runs a command this check never sees"},
		{"FOO=1 $CARGO_TARGET_DIR/debug/desk.exe", "is set before the command"},
	})
	drive(t, NewBoundary("c2", "", []string{"apps/desk/crates/perf/**"}), POSIX, []bashRow{{"cargo build -p perf", shared}, {"cargo test -p perf", ""}})
}

func TestAnMSYSDrivePathIsTheSameFileAsItsWindowsSpelling(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("an MSYS drive path is a Windows spelling")
	}
	scratch := filepath.ToSlash(t.TempDir())
	msys := "/" + strings.ToLower(scratch[:1]) + scratch[2:]
	owner := NewBoundary("rust-dev-92", scratch, []string{"src/**"})
	drive(t, owner, POSIX, []bashRow{
		{"cargo clippy 2>&1 | tee " + msys + "/tmp/clippy1.log", ""},
		{"echo x > /z" + scratch[2:] + "/x.log", notOwned},
	})
	if len(owner.Asked()) != 1 {
		t.Errorf("asked %v, want only the write on another drive", owner.Asked())
	}
}

func TestTheSessionsReadOnlyPipelinesZigAndTheProjectsOwnBinaryRun(t *testing.T) {
	root := lay(t, "compiler/build.zig", "compiler/src/main.zig", "compiler/zig-out/bin/lang", "compiler/examples/run/factorial/k7/project.k7", "target/release/tool")
	pad := root + "/home/.tofu/projects/p/scratchpad"
	scratch, lead := pad+"/sessions/s/agents/research-87", pad+"/sessions/s/lead/tmp/cd"
	if err := os.MkdirAll(scratch+"/tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scratch+"/tmp/check.py", []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reader := NewBoundary("research-87", scratch, nil)
	drive(t, reader, POSIX, []bashRow{
		{`grep -c "testcase(" .local/sources/sqlite/src/*.c --no-filename | paste -sd+ | bc`, ""},
		{`find .local/sources/wails/v3/internal/generator -name "*.go" ! -name "*_test.go" | xargs wc -l | tail -1; find .local/sources/wails/v3/internal/runtime/desktop/@wailsio/runtime/src -name "*.ts" ! -name "*.test.*" | xargs wc -l | tail -1; find .local/sources/wails/v3/pkg/application -name "*.go" ! -name "*_test.go" | xargs wc -l | tail -1; ls .local/sources/wails/v3/internal/generator/testdata/output | head`, ""},
		{`find .local/sources/bun -name '*.zig' -not -path '*/node_modules/*' | wc -l; find .local/sources/bun/src -name '*.rs' | xargs wc -l | tail -1; ls .local/sources/bun/src/runtime .local/sources/bun/src/bun_alloc .local/sources/bun/src/sql .local/sources/bun/bench | head -120`, ""},
		{`find .local/sources/react/compiler/packages/babel-plugin-react-compiler/src -name "*.ts" -not -path "*__tests__*" -exec wc -l {} +`, ""},
		{`git -C .local/sources/systemd grep -n -E "\[UNIT_(REQUIRES|WANTS|REQUISITE|BINDS_TO|PART_OF|UPHOLDS|CONFLICTS|AFTER|BEFORE)\]" -A 4 HEAD -- src/core/unit-dependency-atom.c`, ""},
		{`cd .local/sources/tvm && grep -n -E "kMaxFusedOps|CombinePattern|Phase 0|LegalizeOps\(\)  |never initiate" docs/arch/fusion.rst src/relax/transform/fuse_ops.cc src/relax/analysis/graph_partitioner.h | head; grep -n -E "ApplyDatabase|QueryIRModule|Warning|LOG\(WARNING|not found|enable_warning" src/relax/transform/meta_schedule.cc | head -20; grep -n -E "kVerifySRefTree|DebugVerify" src/s_tir/schedule/state.cc | head; grep -rn -c "TVM_REGISTER_INST_KIND_TRAITS" src/s_tir/schedule/primitive | awk -F: '{s+=$2} END {print "inst_kinds", s}'`, ""},
		{`cat .local/sources/refinedrust/case_studies/minivec/output/minivec/.gitignore; find .local/sources/refinedrust/case_studies -path "*proofs/proof_*.v" -exec wc -l {} + | sort -n | awk '{print $1}' | uniq -c | head -50`, ""},
		{`cat .local/sources/refinedrust/case_studies/minivec/output/minivec/.gitignore; wc -l .local/sources/refinedrust/case_studies/*/output/*/proofs/proof_*.v | sort -n | awk '{print $1}' | uniq -c`, ""},
		{`grep -l "Admitted" .local/sources/refinedrust/case_studies/*/output/*/proofs/proof_*.v; for d in minivec linkedlist evenint hillel knights_tour verifythis2026 iterators; do echo $d; cat .local/sources/refinedrust/case_studies/$d/src/*.rs | grep -c "rr::"; cat .local/sources/refinedrust/case_studies/$d/src/*.rs | grep -cv "^\s*\(//.*\)\?$"; done`, ""},
		{`find .local/sources/refinedrust/rr_frontend -name "*.rs" -not -path "*/target/*" | wc -l; find .local/sources/refinedrust/rr_frontend/translation/src -name "*.rs" | xargs cat | wc -l`, ""},
		{`cd .local/sources/rust/compiler/rustc_borrowck/src && wc -l universal_regions.rs handle_placeholders.rs type_check/liveness/*.rs polonius/*.rs lib.rs nll.rs | sort -n; find diagnostics -name "*.rs" | xargs wc -l | tail -1; find . -name "*.rs" | wc -l`, ""},
		{`ls .local/sources/results/ | wc -l; ls .local/sources/results/ | grep -v research.md; grep -c "" .local/sources/results/*-research.md | awk -F: '{s+=$2} END {print s}'`, ""},
		{`cd compiler/examples/fail && for d in io-missing io-usage io-pmap generic-sum-arity; do echo "== $d"; cat $d/expect.err; ls $d; done`, ""},
		{"awk '/^```/{if(inb){inb=0;n++;next}else{inb=1;lang=substr($0,4);next}} inb{print n\"\\t\"lang\"\\t\"$0}' compiler/docs/agent/key7.md > $TMPDIR/blocks.tsv; wc -l $TMPDIR/blocks.tsv; cut -f1,2 $TMPDIR/blocks.tsv | uniq | wc -l", ""},
		{`python $TMPDIR/check.py`, asksPerson},
		{`cd .local/sources/results && for p in "ttl\b\|eviction\|staleness" "wall.clock" "journal"; do printf "%s: " "$p"; grep -l -i "$p" *.md | wc -l; done`, ""},
		{`rtk ls -R "` + lead + `"; cat "` + lead + `/project.k7" "` + lead + `/src/main.k7"`, ""},
		{`compiler/zig-out/bin/lang print --syntax key1 compiler/examples/run/factorial; echo "exit $?"; compiler/zig-out/bin/lang print --syntax key1 compiler/examples/run/str-basics`, ""},
		{`cat compiler/examples/run/factorial/k7/project.k7; compiler/zig-out/bin/lang 2>&1 | head -20`, ""},
		{"cd compiler && ./zig-out/bin/lang check examples/run/factorial > $TMPDIR/out.txt", ""},
		{"target/release/tool --list", ""},
		{"cd compiler && zig build test", ""},
		{"cd compiler && zig build test --summary all 2>&1 | tail -5", ""},
		{"zig version", ""},
		{"zig fmt --check compiler/src", ""},
	})
	const (
		holdNothing = "holds nothing in"
		noBuild     = "no build.zig"
	)
	drive(t, reader, POSIX, []bashRow{
		{`awk 'BEGIN { system("rm -rf src") }'`, "system"},
		{`awk '{ print | "sh" }' README.md`, notRead},
		{`awk '{ "date" | getline d; print d }' README.md`, notRead},
		{`awk '{ print > "/tmp/x" }' README.md`, notRead},
		{`awk -f prog.awk README.md`, notRead},
		{`gawk -i inplace '{ print }' README.md`, notRead},
		{`awk '{ print }' README.md > /tmp/x.txt`, notOwned},
		{`grep -c x README.md | paste -sd+ | bc > /tmp/sum.txt`, notOwned},
		{"xargs rm < list.txt", notRead},
		{"find . -name '*.o' | xargs rm -f", notRead},
		{"find . -name '*.o' | xargs -I{} cp {} /tmp", notRead},
		{"ls | xargs sh -c 'rm x'", notRead},
		{"ls | xargs sed -i s/a/b/", notRead},
		{"ls | xargs git log", notRead},
		{"find . -name '*.o' -exec rm {} +", notRead},
		{"find . -name '*.o' -exec sh -c 'rm x' \\;", notRead},
		{"find . -ok cat {} \\;", notRead},
		{"for f in $(rm -rf src); do cat $f; done", notRead},
		{"for f in a; do rm $f; done", unreadable},
		{"for f in a; do echo x > out.txt; done", notOwned},
		{"for p in rm; do $p x; done", notRead},
		{"cd compiler && zig build", holdNothing},
		{"zig build", noBuild},
		{"cd compiler && zig build --prefix /tmp/out", notRead},
		{"cd compiler && zig build -p out install", notRead},
		{"zig build --build-file /tmp/build.zig test", notRead},
		{"zig run x.zig", notRead},
		{"zig cc -o x x.c", notRead},
		{"zig fmt compiler/src/main.zig", notOwned},
		{"cd /tmp && zig build test", notRead},
		{"compiler/zig-out/bin/lang ../x", notRead},
		{"../zig-out/bin/lang", "is not a bare program name"},
		{root + "/compiler/zig-out/bin/lang", "is not a bare program name"},
		{"compiler/src/lang", "is not a bare program name"},
		{"compiler/zig-out/bin/lang $(rm -rf src)", "runs a command this check never sees"},
	})
	drive(t, NewBoundary("zig-dev-1", scratch, []string{"compiler/src/**"}), POSIX, []bashRow{
		{"cd compiler && zig build", ""},
		{"cd compiler && zig build -Doptimize=ReleaseFast run -- examples/run/factorial", ""},
		{"zig fmt compiler/src/main.zig", ""},
		{"zig fmt compiler/build.zig", notOwned},
	})
}
