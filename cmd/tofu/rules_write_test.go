package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/sys"
	"tofu/internal/turn"
)

func tofuRules(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(append([]string{"rules"}, args...), strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func listedRule(t *testing.T, id string) ruleListing {
	t.Helper()
	code, out, errOut := tofuRules(t, "list", jsonFlag)
	var envelope struct {
		Data ruleListReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); code != exitOK || err != nil {
		t.Fatalf("rules list --json exited %d, %v: %s", code, err, errOut)
	}
	at := slices.IndexFunc(envelope.Data.Rules, func(r ruleListing) bool { return r.ID == id })
	if at < 0 {
		return ruleListing{}
	}
	return envelope.Data.Rules[at]
}

func TestRulesAddOffAndRemoveWriteTheHomeAndTheProjectLayers(t *testing.T) {
	project := chdirTemp(t)
	home := os.Getenv("USERPROFILE")

	if code, out, errOut := tofuRules(t, "add", "--global", "g1", "never write yaml by hand"); code != exitOK || !strings.Contains(out, "tofu rules remove --global g1") {
		t.Fatalf("add --global exited %d, out %q, err %q", code, out, errOut)
	}
	globalFile := filepath.Join(home, ".tofu", "rules", "g1@1.yaml")
	if listed := listedRule(t, "g1"); listed.Origin != "global" || listed.File != globalFile {
		t.Errorf("rules list shows g1 as %+v, want global and %s", listed, globalFile)
	}

	if code, _, errOut := tofuRules(t, "add", "p1", "read the ticket first"); code != exitOK {
		t.Fatalf("add exited %d: %s", code, errOut)
	}
	if listed := listedRule(t, "p1"); listed.Origin != "project" || listed.File != filepath.Join(project, ".tofu", "rules", "p1@1.yaml") {
		t.Errorf("rules list shows p1 as %+v, want project and its file", listed)
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

	if listedRule(t, "em_dash").ID == "" {
		t.Fatal("em_dash is not listed before the off, so the test proves nothing")
	}
	if code, _, errOut := tofuRules(t, "off", "em_dash", "--reason", "quoted prose"); code != exitOK {
		t.Fatalf("off exited %d: %s", code, errOut)
	}
	if listed := listedRule(t, "em_dash"); listed.Mode != "off" || listed.Override == nil {
		t.Errorf("em_dash after off is listed as %+v, want it marked off by an override", listed)
	}
	if code, _, errOut := tofuRules(t, "remove", "em_dash"); code != exitOK {
		t.Fatalf("remove of the off exited %d: %s", code, errOut)
	}
	if listed := listedRule(t, "em_dash"); listed.Origin != "shipped" {
		t.Errorf("em_dash after remove is listed as %+v, want shipped", listed)
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
	if _, err := reloadReport(project); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := tofuRules(t, "add", "p1", "answer in one line"); code != exitOK {
		t.Fatalf("add exited %d: %s", code, errOut)
	}
	after, err := reloadReport(project)
	if err != nil {
		t.Fatal(err)
	}
	if at := slices.IndexFunc(after.Parts, func(part partDiff) bool { return part.Name == "rules" }); at < 0 || !slices.Equal(after.Parts[at].Added, []string{"p1"}) {
		t.Errorf("reload after the add reads %+v, want the rules part to add p1 alone", after.Parts)
	}
	prompt, err := composeRun(runOpts{dir: project}, nil, runtime{open: openAppWire})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(prompt.composed.Parts, func(part turn.PromptPart) bool { return part.RuleID == "p1" && part.Text == "answer in one line" }) {
		t.Error("the composed prompt does not carry the project rule p1")
	}
}

func overridesListed(t *testing.T) []overrideListing {
	t.Helper()
	code, out, errOut := tofuRules(t, "overrides", jsonFlag)
	var envelope struct {
		Data overridesReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); code != exitOK || err != nil {
		t.Fatalf("rules overrides --json exited %d, %v: %s%s", code, err, out, errOut)
	}
	return envelope.Data.Overrides
}

func TestRulesOffNeedsAReasonIsMarkedEverywhereAndRestoreBringsTheRuleBack(t *testing.T) {
	project := chdirTemp(t)
	id := "no_unit_test_after_code"
	file := filepath.Join(project, ".tofu", "rules", id+"@1.yaml")

	if code, _, errOut := tofuRules(t, "off", "--project", id); code == exitOK || !strings.Contains(errOut, "--reason") {
		t.Errorf("off without a reason exited %d, err %q, want a refusal naming --reason", code, errOut)
	}
	if code, _, errOut := tofuRules(t, "off", "--project", "--global", "--reason", "x", id); code == exitOK {
		t.Errorf("off with --project and --global exited %d: %s", code, errOut)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("a refused off wrote %s: %v", file, err)
	}

	if code, out, errOut := tofuRules(t, "off", id, "--project", "--reason", "public SDK"); code != exitOK || !strings.Contains(out, "tofu rules restore "+id) {
		t.Fatalf("off exited %d, out %q, err %q, want the undo to name restore", code, out, errOut)
	}
	written, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"overrides: " + id + "@1", "mode: off", "reason: public SDK", "by: person", "at: "} {
		if !strings.Contains(string(written), line) {
			t.Errorf("%s lacks %q:\n%s", file, line, written)
		}
	}

	listed := overridesListed(t)
	if len(listed) != 1 || listed[0].RuleID != id || listed[0].Layer != "project" || listed[0].Reason != "public SDK" || listed[0].Stale || listed[0].File != file {
		t.Errorf("rules overrides = %+v, want %s from the project with its reason", listed, id)
	}
	if marked := listedRule(t, id); marked.Override == nil || marked.Override.Reason != "public SDK" || marked.Origin != "project" {
		t.Errorf("rules list shows %s as %+v, want it marked with the project and its reason", id, marked)
	}
	code, out, errOut := tofuRules(t, "index", "add unit tests for the parser", "parser_test.go", jsonFlag)
	var index struct{ Data ruleIndexReport }
	if err := json.Unmarshal([]byte(out), &index); code != exitOK || err != nil {
		t.Fatalf("rules index exited %d (%v): %s%s", code, err, out, errOut)
	}
	if at := slices.IndexFunc(index.Data.Rules, func(r ruleIndexListing) bool { return r.RuleID == id }); at < 0 || index.Data.Rules[at].Fires || index.Data.Rules[at].Override == nil || index.Data.Rules[at].Override.Layer != "project" {
		t.Errorf("rules index does not mark %s as overridden in the project: %+v", id, index.Data.Rules)
	}

	if code, out, errOut := tofuRules(t, "restore", id); code != exitOK || !strings.Contains(out, "--reason") {
		t.Fatalf("restore exited %d, out %q, err %q, want an undo carrying the reason", code, out, errOut)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("restore left %s: %v", file, err)
	}
	if listed := listedRule(t, id); listed.Origin != "shipped" || listed.Override != nil {
		t.Errorf("after restore %s is listed as %+v, want shipped and unmarked", id, listed)
	}
	if code, _, _ := tofuRules(t, "restore", id); code == exitOK {
		t.Error("a second restore found something to restore")
	}
	if code, _, errOut := tofuRules(t, "add", "p1", "read the ticket first"); code != exitOK {
		t.Fatalf("add exited %d: %s", code, errOut)
	}
	if code, _, errOut := tofuRules(t, "restore", "p1"); code == exitOK || !strings.Contains(errOut, "tofu rules remove p1") {
		t.Errorf("restore of a rule of the person's own exited %d, err %q, want a refusal naming remove", code, errOut)
	}
}

func TestRulesAddOverALibraryRuleNeedsAReasonAndWritesAnOverride(t *testing.T) {
	project := chdirTemp(t)
	id := "no_unit_test_after_code"
	if code, _, errOut := tofuRules(t, "add", id, "unit tests are the contract here"); code == exitOK || !strings.Contains(errOut, "--reason") {
		t.Errorf("add over a library rule without a reason exited %d, err %q", code, errOut)
	}
	if code, _, errOut := tofuRules(t, "add", "--reason", "public SDK", id, "unit tests are the contract here"); code != exitOK {
		t.Fatalf("add --reason exited %d: %s", code, errOut)
	}
	written, err := os.ReadFile(filepath.Join(project, ".tofu", "rules", id+"@1.yaml"))
	if err != nil || !strings.Contains(string(written), "overrides: "+id+"@1") || !strings.Contains(string(written), "text: unit tests are the contract here") || strings.Contains(string(written), "domain:") {
		t.Errorf("add --reason wrote %q (%v), want an override carrying the text and nothing of the base", written, err)
	}
	if listed := overridesListed(t); len(listed) != 1 || listed[0].Change != "text" {
		t.Errorf("rules overrides = %+v, want one text override", listed)
	}
}

func TestRulesOverridesNamesAnOverrideTheLibraryMovedPast(t *testing.T) {
	project := chdirTemp(t)
	files := map[string]string{
		".tofu/library/qa/rules/no_unit_test_after_code@2.yaml": "id: no_unit_test_after_code\ndomain: qa\nkind: human\nconcern: code_rules\ntext: never a unit test after the code\n",
		".tofu/rules/no_unit_test_after_code@1.yaml":            "id: no_unit_test_after_code\noverrides: no_unit_test_after_code@1\nmode: off\nreason: public SDK\nby: person\nat: 2026-10-05\n",
	}
	for name, body := range files {
		if err := sys.WriteFile(filepath.Join(project, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if listed := overridesListed(t); len(listed) != 1 || !listed[0].Stale || listed[0].Version != 1 || listed[0].Current != 2 {
		t.Errorf("rules overrides = %+v, want one stale override of @1 against @2", listed)
	}
	if listed := listedRule(t, "no_unit_test_after_code"); listed.ID == "" {
		t.Error("a stale override still switched the rule off")
	}
}
