package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func tofuAgents(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(append([]string{"agents"}, args...), strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func agentsLine(t *testing.T, name string) string {
	t.Helper()
	_, out, _ := tofuAgents(t)
	for _, line := range strings.Split(out, "\n") {
		if slices.Contains(strings.Fields(line), name) {
			return line
		}
	}
	return ""
}

func TestAgentsAddSetAndRemoveWriteTheHomeAndTheProjectLayers(t *testing.T) {
	project := chdirTemp(t)
	home := os.Getenv("USERPROFILE")
	added := map[string]string{"planner": "claude-sub/claude-opus-5", "coder": "claude-sub/claude-sonnet-5", "checker": "codex-sub/gpt-5.6-sol"}
	for name, model := range added {
		code, out, errOut := tofuAgents(t, "add", name, "--description", "plans: \"the work\"", "--model", model, "--tools", "read, search")
		if code != exitOK || !strings.Contains(out, "undo: tofu agents remove "+name) {
			t.Fatalf("add %s exited %d, out %q, err %q", name, code, out, errOut)
		}
	}
	for name, model := range added {
		if _, err := os.Stat(filepath.Join(project, ".tofu", "agents", name+".md")); err != nil {
			t.Errorf("add %s wrote no file in the project: %v", name, err)
		}
		if line := agentsLine(t, name); !strings.Contains(line, model) || !slices.Contains(strings.Fields(line), "project") {
			t.Errorf("tofu agents shows %s as %q, want %s and project", name, line, model)
		}
	}
	found, err := agentsIn(project)
	if planner, listed := listedAgent(found, "planner"); err != nil || !listed || planner.Description != `plans: "the work"` || !slices.Equal(planner.Tools, []string{"read", "search"}) {
		t.Errorf("the description or the tools did not survive the round trip: %+v, %v", planner, err)
	}

	if code, _, errOut := tofuAgents(t, "add", "planner", "--description", "again", "--model", "claude-sub/claude-opus-5"); code != exitVerdict || !strings.Contains(errOut, "planner") {
		t.Errorf("a second add of planner exited %d, err %q, want a refusal", code, errOut)
	}
	for _, name := range []string{"../escape", "Planner", "two words", ""} {
		if code, _, errOut := tofuAgents(t, "add", name, "--description", "d", "--model", "claude-sub/claude-opus-5"); code == exitOK {
			t.Errorf("the name %q was accepted: %s", name, errOut)
		}
	}
	if _, err := os.Stat(filepath.Join(project, ".tofu", "escape.md")); !os.IsNotExist(err) {
		t.Errorf("a name with a path in it wrote a file: %v", err)
	}
	if code, _, errOut := tofuAgents(t, "add", "ghost", "--description", "d", "--model", "claude-sub/claude-nothing-9"); code != exitVerdict {
		t.Errorf("an unknown slug on add exited %d, err %q, want 1", code, errOut)
	}
	if code, _, errOut := tofuAgents(t, "add", "ghost", "--description", "d", "--model", "claude-sub/claude-opus-5", "--tools", "teleport"); code != exitVerdict || !strings.Contains(errOut, "teleport") {
		t.Errorf("an unknown tool on add exited %d, err %q, want 1 naming it", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(project, ".tofu", "agents", "ghost.md")); !os.IsNotExist(err) {
		t.Errorf("a refused add left its file behind: %v", err)
	}

	if code, out, errOut := tofuAgents(t, "set", "planner", "codex-sub/gpt-5.6-sol"); code != exitOK || !strings.Contains(out, "undo: tofu agents set planner claude-sub/claude-opus-5") {
		t.Fatalf("set exited %d, out %q, err %q", code, out, errOut)
	}
	if line := agentsLine(t, "planner"); !strings.Contains(line, "codex-sub/gpt-5.6-sol") || !slices.Contains(strings.Fields(line), "project") {
		t.Errorf("after set, tofu agents shows planner as %q, want codex-sub/gpt-5.6-sol and project", line)
	}
	assigned, err := os.ReadFile(filepath.Join(project, ".tofu", "agent-models.yaml"))
	if err != nil || !strings.Contains(string(assigned), "planner: codex-sub/gpt-5.6-sol") {
		t.Errorf("agent-models.yaml reads %q, %v", assigned, err)
	}
	if code, _, errOut := tofuAgents(t, "set", "qa", "claude-sub/claude-sonnet-5"); code != exitOK {
		t.Errorf("set on a library agent exited %d: %s", code, errOut)
	}
	if code, _, errOut := tofuAgents(t, "set", "planner", "claude-sub/claude-nothing-9"); code != exitVerdict {
		t.Errorf("an unknown slug on set exited %d, err %q, want 1", code, errOut)
	}
	if code, _, errOut := tofuAgents(t, "set", "nobody", "claude-sub/claude-opus-5"); code != exitVerdict {
		t.Errorf("set on an agent nobody lists exited %d, err %q, want 1", code, errOut)
	}

	if code, _, errOut := tofuAgents(t, "remove", "qa"); code != exitVerdict || !strings.Contains(errOut, "tofu agents set qa") {
		t.Errorf("remove of a library agent exited %d, err %q, want 1 naming set", code, errOut)
	}
	if code, out, errOut := tofuAgents(t, "remove", "planner"); code != exitOK || !strings.Contains(out, "tofu agents add planner") {
		t.Fatalf("remove exited %d, out %q, err %q", code, out, errOut)
	}
	if line := agentsLine(t, "planner"); line != "" {
		t.Errorf("planner is still listed after remove: %q", line)
	}
	assigned, _ = os.ReadFile(filepath.Join(project, ".tofu", "agent-models.yaml"))
	if strings.Contains(string(assigned), "planner") || !strings.Contains(string(assigned), "qa: claude-sub/claude-sonnet-5") {
		t.Errorf("after remove, agent-models.yaml reads %q, want planner gone and qa kept", assigned)
	}

	if code, _, errOut := tofuAgents(t, "add", "--global", "reviewer", "--description", "reviews", "--model", "claude-sub/claude-opus-5"); code != exitOK {
		t.Fatalf("add --global exited %d: %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(home, ".tofu", "agents", "reviewer.md")); err != nil {
		t.Errorf("add --global wrote no file in the home: %v", err)
	}
	if line := agentsLine(t, "reviewer"); !slices.Contains(strings.Fields(line), "global") {
		t.Errorf("tofu agents shows reviewer as %q, want global", line)
	}
	if code, _, _ := tofuAgents(t, "remove", "reviewer"); code == exitOK {
		t.Error("remove without --global deleted an agent from the home layer")
	}
	if code, _, errOut := tofuAgents(t, "remove", "--global", "reviewer"); code != exitOK {
		t.Errorf("remove --global exited %d: %s", code, errOut)
	}
}

func TestAgentsAddWithDirWritesThatProjectFromAnotherDirectory(t *testing.T) {
	project := chdirTemp(t)
	t.Chdir(t.TempDir())
	if code, out, errOut := tofuAgents(t, "add", "--dir", project, "planner", "--description", "plans", "--model", "claude-sub/claude-opus-5"); code != exitOK || !strings.Contains(out, "tofu agents remove --dir "+project+" planner") {
		t.Fatalf("add --dir exited %d, out %q, err %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(project, ".tofu", "agents", "planner.md")); err != nil {
		t.Errorf("add --dir did not write into the project: %v", err)
	}
}
