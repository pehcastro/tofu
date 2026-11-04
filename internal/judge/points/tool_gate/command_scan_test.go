package toolgate

import "testing"

func TestCommandScanSeesInterpreterCodeWhereverItComesFrom(t *testing.T) {
	where := roots{Cwd: "/repo", Project: "/repo"}
	for _, c := range []struct {
		name    string
		command string
		want    Determination
	}{
		{"heredoc with a dash", "python - <<EOF\nimport shutil\nshutil.rmtree('/repo')\nEOF", TargetUnknown},
		{"heredoc without a dash", "python <<EOF\nprint(1)\nEOF", TargetUnknown},
		{"node heredoc", "node - <<'EOF'\nrequire('fs').rmSync('/repo',{recursive:true})\nEOF", TargetUnknown},
		{"stdin through a pipe", "echo 'import os; os.remove(\"x\")' | python", TargetUnknown},
		{"stdin dash through a pipe", "cat x.py | python3 -", TargetUnknown},
		{"versioned interpreter", `python3.12 -c "print(1)"`, TargetUnknown},
		{"uv run wrapper", `uv run python -c "print(1)"`, TargetUnknown},
		{"env wrapper", `env python -c "print(1)"`, TargetUnknown},
		{"env wrapper with an assignment", `env FOO=1 python3 -c "print(1)"`, TargetUnknown},
		{"npx wrapper", `npx node -e "1"`, TargetUnknown},
		{"xargs wrapper", "ls | xargs python", TargetUnknown},
		{"eval", `eval "$(cat x)"`, TargetUnknown},
		{"source", "source x.sh", TargetUnknown},
		{"deno eval", `deno eval "1"`, TargetUnknown},
		{"bun -e", `bun -e "1"`, TargetUnknown},
		{"Rscript -e", `Rscript -e "1"`, TargetUnknown},
		{"windows path and exe on stdin", `C:\Python312\python.exe -`, TargetUnknown},
		{"unix path on stdin", "/usr/bin/python3 -", TargetUnknown},
		{"interpreter after cd", "cd sub && python - <<EOF\nprint(1)\nEOF", TargetUnknown},
		{"second heredoc feeds the interpreter", "cat <<A > /repo/a.txt\nx\nA\nbash <<B\nrm -rf /\nB", TargetUnknown},
		{"listing", "ls -la", NoWriteFound},
		{"interpreter name as an argument", "grep python README.md", NoWriteFound},
		{"read-only git", "git status", NoWriteFound},
		{"redirect", "echo hi > out.txt", TargetsResolved},
	} {
		if got := scanCommand(c.command, where).Determination; got != c.want {
			t.Errorf("%s: %q answers %s, want %s", c.name, c.command, got, c.want)
		}
	}
}

func TestCommandScanKeepsTheTargetsItCanNameInInterpreterCode(t *testing.T) {
	found := scanCommand("python - <<EOF\nopen('a.txt', 'w').write('x')\nEOF", roots{Cwd: "/repo", Project: "/repo"})
	if found.Determination != TargetUnknown || len(found.Targets) != 1 || found.Targets[0].Path != "/repo/a.txt" {
		t.Fatalf("a python heredoc writing a.txt answers %+v, want writes_but_target_unknown still naming /repo/a.txt", found)
	}
}
