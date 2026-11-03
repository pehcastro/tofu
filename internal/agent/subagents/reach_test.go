package subagent

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
		{"apps/desk/target/debug/desk.exe", "is not a bare program name"},
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
