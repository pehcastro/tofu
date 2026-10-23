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
		{"CARGO_TARGET_DIR=" + scratch + "/target cargo clippy", ""},
		{"CARGO_TARGET_DIR=$TMPDIR/target cargo test", ""},
		{"CARGO_TARGET_DIR=/elsewhere/target cargo test", notOwned},
		{"cargo build", ""},
		{"cargo build --target-dir target/alt", ""},
		{"cargo build --target-dir=out", notOwned},
		{"rustfmt src/a.rs", ""},
		{"rustfmt --check README.rs", ""},
		{"rustfmt lib.rs", notOwned},
	})
	drive(t, NewBoundary("rust-dev-2", scratch, []string{"src/**"}), POSIX, []bashRow{{"cargo build", notOwned}})
	drive(t, owner, PowerShell, []bashRow{
		{"cargo test 2>&1 | tail -5; echo ${PIPESTATUS[0]}", ""},
		{"echo ${c:/README.md}", notRead},
		{"echo ${PIPESTATUS[0]} | ForEach-Object { rm x }", notRead},
	})
}

func TestAScriptInAHeldFolderRunsAndATimeoutIsJudgedByWhatItWraps(t *testing.T) {
	root := lay(t, "Cargo.toml", "src/main.rs", "shots/d1/run.ps1", "shots/other/run.ps1", "run.ps1")
	scratch := root + "/.tofu/scratch/rust-dev-7"
	owner := NewBoundary("rust-dev-7", scratch, []string{"src/**", "target_agent_7/**", "shots/d1/**", "run.ps1"})
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
		{"timeout -k 5 --signal=KILL 60 cargo build --target-dir target_agent_7", ""},
		{"timeout 600 powershell -NoProfile -File shots/d1/run.ps1", ""},
		{"rtk proxy timeout 60 powershell -File shots/d1/run.ps1", ""},
		{"timeout 60 cargo build --target-dir target", notOwned},
		{"timeout 60 powershell -File shots/other/run.ps1", askGrant},
		{`timeout 60 powershell -Command "Remove-Item C:/x"`, notRead},
		{"timeout 5 curl http://x", notRead},
		{"timeout 5", notRead},
		{"timeout 5 timeout 5 gofmt -w README.go", notOwned},
		{"cargo build --target-dir target_agent_7", ""},
		{"cargo build --target-dir target", notOwned},
	}
	drive(t, owner, POSIX, rows)
	drive(t, owner, PowerShell, rows)
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
