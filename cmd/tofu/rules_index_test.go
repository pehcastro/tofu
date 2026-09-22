package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTriggerLibrary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"scoped@1.yaml":      "id: scoped\ndomain: dev\nkind: structural\nconcern: code_rules\nchecker: comments\nscope: internal/judge/**\n",
		"conditioned@1.yaml": "id: conditioned\ndomain: dev\nkind: structural\nconcern: process_discipline\nchecker: comments\ncondition: force.?push\n",
		"golang@1.yaml":      "id: golang\ndomain: dev\nkind: structural\nconcern: code_rules\nchecker: comments\nlanguage: go\ntask: write\n",
		"em_dash@1.yaml":     enforcedEmDashRule,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

func TestRulesIndexSeparatesAScopeThatReachedNothingFromAConditionThatDidNotMatch(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesIndexVerb([]string{"rename the trigger", "cmd/tofu/rules.go", "--task", "write", "--library", writeTriggerLibrary(t)}, out, errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	body := out.String()
	for _, want := range []string{
		"the scope internal/judge/** reaches none of the paths the task names",
		"the condition force.?push matches nothing in the task",
		"always on, the rule declares no trigger",
		"the language go reached cmd/tofu/rules.go and the task is write",
		"2 of 4 rules fire",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the report does not say %q:\n%s", want, body)
		}
	}
}

func TestRulesIndexAnswersATaskThatNamesNoPaths(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesIndexVerb([]string{"force push the branch", "--library", writeTriggerLibrary(t)}, out, errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	body := out.String()
	if !strings.Contains(body, "paths: none, so a scope and a language reach nothing and hold their rule back") {
		t.Fatalf("the report does not say what a scope does with no paths:\n%s", body)
	}
	if !strings.Contains(body, "kind: unnamed") {
		t.Fatalf("the report does not say the task kind is unnamed:\n%s", body)
	}
	if !strings.Contains(body, "the condition force.?push matched") {
		t.Fatalf("a condition alone did not match without paths:\n%s", body)
	}
	if !strings.Contains(body, "the scope internal/judge/** reaches none of the paths the task names") {
		t.Fatalf("the scope did not report reaching nothing:\n%s", body)
	}
}

func TestRulesIndexJSONCarriesTheConcernAndTheReason(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesIndexVerb([]string{"rename the trigger", "internal/judge/policy.go", "--json", "--library", writeTriggerLibrary(t)}, out, errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	var report ruleIndexReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshalling json: %v, body %q", err, out.String())
	}
	if report.Task != "rename the trigger" || len(report.Paths) != 1 {
		t.Fatalf("the report does not carry the task it answered: %+v", report)
	}
	byID := map[string]ruleIndexListing{}
	for _, one := range report.Rules {
		byID[one.RuleID] = one
	}
	scoped := byID["scoped"]
	if !scoped.Fires || scoped.Concern != "code_rules" || scoped.Mode != "shadow" {
		t.Fatalf("the scoped rule reads %+v, want a firing code_rules rule in shadow", scoped)
	}
	if byID["golang"].Fires {
		t.Fatalf("the go rule fired on a task that is not a write: %+v", byID["golang"])
	}
	if report.Firing != 2 {
		t.Fatalf("firing = %d, want 2: %+v", report.Firing, report.Rules)
	}
}

func TestRulesIndexRefusesATaskItCannotRead(t *testing.T) {
	if _, err := parseRulesIndexArgs([]string{"--library", "x"}); err == nil {
		t.Fatal("parseRulesIndexArgs accepted no task text")
	}
	if _, err := parseRulesIndexArgs([]string{"a task", "--task", "sing"}); err == nil {
		t.Fatal("parseRulesIndexArgs accepted an unknown task kind")
	}
	if _, err := parseRulesIndexArgs([]string{"a task", "--task"}); err == nil {
		t.Fatal("parseRulesIndexArgs accepted a bare --task")
	}
	if _, err := parseRulesListArgs([]string{"--task", "write"}); err == nil {
		t.Fatal("tofu rules list accepted --task, a flag only index takes")
	}
}
