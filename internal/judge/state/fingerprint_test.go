package state

import "testing"

func bashCall(command string) ToolGateInput {
	return ToolGateInput{
		Agent:      "tofu-run",
		Tool:       "bash",
		Input:      map[string]any{"command": command},
		Cwd:        `F:\localhost\ephem-sh\bob`,
		ProjectDir: `F:\localhost\ephem-sh\bob`,
	}
}

func TestOnlyTheConcreteValueChangesAndTheFingerprintHolds(t *testing.T) {
	cases := []struct {
		what  string
		left  ToolGateInput
		right ToolGateInput
	}{
		{
			what:  "the ref",
			left:  bashCall("git push --force origin main"),
			right: bashCall("git push --force origin release/0.4"),
		},
		{
			what: "the file path",
			left: ToolGateInput{Tool: "write", Input: map[string]any{"file_path": `F:\localhost\ephem-sh\bob\internal\judge\ledger\read.go`, "content": "package ledger"},
				Cwd: `F:\localhost\ephem-sh\bob`, ProjectDir: `F:\localhost\ephem-sh\bob`},
			right: ToolGateInput{Tool: "write", Input: map[string]any{"file_path": `F:\localhost\ephem-sh\bob\internal\turn\loop.go`, "content": "package turn"},
				Cwd: `F:\localhost\ephem-sh\bob`, ProjectDir: `F:\localhost\ephem-sh\bob`},
		},
		{
			what:  "the branch name",
			left:  bashCall("git checkout main"),
			right: bashCall("git checkout feature/precedent-retrieval"),
		},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			left, right := FingerprintOf(c.left), FingerprintOf(c.right)
			if left != right {
				t.Fatalf("%s differs and the fingerprint moved: %s vs %s", c.what, left, right)
			}
			t.Logf("%s: both calls fingerprint as %s", c.what, left)
		})
	}
}

func TestGenuinelyDifferentToolsNeverShareAFingerprint(t *testing.T) {
	calls := map[string]ToolGateInput{
		"push":        bashCall("git push --force origin main"),
		"pull":        bashCall("git pull origin main"),
		"status":      bashCall("git status"),
		"remove":      bashCall("rm -rf build/"),
		"install":     bashCall("npm install left-pad"),
		"push twice":  bashCall("git push origin main && git push origin tags"),
		"write":       {Tool: "write", Input: map[string]any{"file_path": "a.go"}},
		"edit":        {Tool: "edit", Input: map[string]any{"file_path": "a.go"}},
		"read":        {Tool: "read", Input: map[string]any{"file_path": "a.go"}},
		"no argument": {Tool: "bash", Input: map[string]any{}},
	}
	seen := make(map[string]string, len(calls))
	for name, call := range calls {
		print := FingerprintOf(call)
		if other, clash := seen[print]; clash {
			t.Fatalf("%q and %q are different calls and share fingerprint %s", name, other, print)
		}
		seen[print] = name
		t.Logf("%-12s %s", name, print)
	}
}

func TestTheFingerprintIsStableAcrossRunsAndAcrossCheckouts(t *testing.T) {
	call := bashCall("git push --force origin main")
	first := FingerprintOf(call)
	for i := 0; i < 64; i++ {
		if again := FingerprintOf(call); again != first {
			t.Fatalf("run %d fingerprinted the same call as %s, first run said %s", i, again, first)
		}
	}

	elsewhere := ToolGateInput{
		Agent:      "tofu-run",
		Tool:       "bash",
		Input:      map[string]any{"command": "git push --force origin main"},
		Cwd:        "/home/luiz/src/bob/internal/judge",
		ProjectDir: "/home/luiz/src/bob",
	}
	here := call
	here.Cwd = `F:\localhost\ephem-sh\bob\internal\judge`
	if FingerprintOf(here) != FingerprintOf(elsewhere) {
		t.Fatalf("the same repository checked out twice fingerprinted as %s and %s", FingerprintOf(here), FingerprintOf(elsewhere))
	}
	if FingerprintOf(here) == first {
		t.Fatalf("a different working directory kept fingerprint %s, so the directory is not in the key", first)
	}
	t.Logf("repository root %s, subdirectory %s", first, FingerprintOf(here))
}

func TestAToolWithNoNameHasNoFingerprint(t *testing.T) {
	if print := FingerprintOf(ToolGateInput{Input: map[string]any{"command": "git status"}}); print != "" {
		t.Fatalf("a call with no tool fingerprinted as %s, want the empty string", print)
	}
}
