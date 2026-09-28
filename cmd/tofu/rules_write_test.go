package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/turn"
)

func tofuRules(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(append([]string{"rules"}, args...), strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func listedRule(t *testing.T, id string) string {
	t.Helper()
	code, out, errOut := tofuRules(t, "list")
	if code != exitOK {
		t.Fatalf("rules list exited %d: %s", code, errOut)
	}
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == id {
			return line
		}
	}
	return ""
}

func TestRulesAddOffAndRemoveWriteTheHomeAndTheProjectLayers(t *testing.T) {
	project := chdirTemp(t)
	home := os.Getenv("USERPROFILE")

	if code, out, errOut := tofuRules(t, "add", "--global", "g1", "never write yaml by hand"); code != exitOK || !strings.Contains(out, "tofu rules remove --global g1") {
		t.Fatalf("add --global exited %d, out %q, err %q", code, out, errOut)
	}
	globalFile := filepath.Join(home, ".tofu", "rules", "g1@1.yaml")
	if line := listedRule(t, "g1"); !slices.Contains(strings.Fields(line), "global") || !strings.Contains(line, globalFile) {
		t.Errorf("rules list shows g1 as %q, want global and %s", line, globalFile)
	}

	if code, _, errOut := tofuRules(t, "add", "p1", "read the ticket first"); code != exitOK {
		t.Fatalf("add exited %d: %s", code, errOut)
	}
	if line := listedRule(t, "p1"); !slices.Contains(strings.Fields(line), "project") || !strings.Contains(line, filepath.Join(project, ".tofu", "rules", "p1@1.yaml")) {
		t.Errorf("rules list shows p1 as %q, want project and its file", line)
	}
	if code, _, errOut := tofuRules(t, "add", "p1", "something else"); code == exitOK || !strings.Contains(errOut, "--replace") {
		t.Errorf("a second add of p1 exited %d, err %q, want a refusal naming --replace", code, errOut)
	}
	if code, _, errOut := tofuRules(t, "add", "../escape", "text"); code == exitOK {
		t.Errorf("an id with a path in it was accepted: %s", errOut)
	}
	if code, _, _ := tofuRules(t, "remove", "g1"); code == exitOK {
		t.Error("remove without --global deleted a rule from the home layer")
	}

	if listedRule(t, "em_dash") == "" {
		t.Fatal("em_dash is not listed before the off, so the test proves nothing")
	}
	if code, _, errOut := tofuRules(t, "off", "em_dash"); code != exitOK {
		t.Fatalf("off exited %d: %s", code, errOut)
	}
	if line := listedRule(t, "em_dash"); line != "" {
		t.Errorf("em_dash is still listed after off: %q", line)
	}
	if code, _, errOut := tofuRules(t, "remove", "em_dash"); code != exitOK {
		t.Fatalf("remove of the off exited %d: %s", code, errOut)
	}
	if line := listedRule(t, "em_dash"); !slices.Contains(strings.Fields(line), "shipped") {
		t.Errorf("em_dash after remove is listed as %q, want shipped", line)
	}
	if code, _, errOut := tofuRules(t, "remove", "em_dash"); code == exitOK || !strings.Contains(errOut, "tofu rules off em_dash") {
		t.Errorf("remove of a shipped rule exited %d, err %q, want a refusal naming off", code, errOut)
	}
}

func TestRunWithDirCarriesThatProjectsRulesFromAnotherDirectory(t *testing.T) {
	project := chdirTemp(t)
	if code, _, errOut := tofuRules(t, "add", "p1", "answer in one line"); code != exitOK {
		t.Fatalf("add exited %d: %s", code, errOut)
	}
	t.Chdir(t.TempDir())
	var out, errOut bytes.Buffer
	if code := run([]string{"run", "--dir", project, "--show-prompt", "x"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("run --show-prompt exited %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "answer in one line") {
		t.Errorf("the prompt for --dir %s does not carry its project rule p1:\n%s", project, out.String())
	}
	if code, _, errOut := tofuRules(t, "remove", "--dir", project, "p1"); code != exitOK {
		t.Fatalf("remove --dir from another directory exited %d: %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(project, ".tofu", "rules", "p1@1.yaml")); !os.IsNotExist(err) {
		t.Errorf("remove --dir left the project rule file: %v", err)
	}
}

func TestThePromptAndReloadSeeTheSameProjectRule(t *testing.T) {
	project := chdirTemp(t)
	before, err := runReload(project)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := tofuRules(t, "add", "p1", "answer in one line"); code != exitOK {
		t.Fatalf("add exited %d: %s", code, errOut)
	}
	after, err := runReload(project)
	if err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Errorf("reload counted %d rules before the add and %d after, want one more", before, after)
	}
	prompt, err := composeRun(runOpts{dir: project}, nil, runtime{open: openAppWire})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(prompt.composed.Parts, func(part turn.PromptPart) bool { return part.RuleID == "p1" && part.Text == "answer in one line" }) {
		t.Error("the composed prompt does not carry the project rule p1")
	}
}
