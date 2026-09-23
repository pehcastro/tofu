package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tofu/internal/rule"
	"tofu/internal/sys"
)

var firedAt = time.Date(2026, 9, 21, 15, 4, 5, 0, time.UTC)

func recordedFires(t *testing.T) string {
	t.Helper()
	isolatedHomeAndProject(t)
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	for _, fire := range []rule.Fire{
		{RuleID: "em_dash", Mode: rule.ModeEnforced, Target: "internal/one.go", Blocked: true, At: firedAt},
		{RuleID: "test_assertion", Mode: rule.ModeShadow, Target: "internal/two.go", At: firedAt.Add(time.Minute)},
	} {
		if err := appendRuleFire(dir, fire); err != nil {
			t.Fatalf("appendRuleFire: %v", err)
		}
	}
	return dir
}

func TestRulesFiredReadsBackTheRecordRulesCheckWrote(t *testing.T) {
	recordedFires(t)
	var out, errOut bytes.Buffer
	if code := rulesFiredVerb([]string{jsonFlag}, &out, &errOut); code != exitOK {
		t.Fatalf("rules fired exited %d: %s", code, errOut.String())
	}
	var report ruleFiredReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("reading the fired report: %v, body %q", err, out.String())
	}
	var ids []string
	for _, fire := range report.Fires {
		ids = append(ids, fire.RuleID)
	}
	if strings.Join(ids, ",") != "em_dash,test_assertion" {
		t.Fatalf("the rule ids read back as %v, want the two that were written", ids)
	}
	if report.Blocked != 1 {
		t.Fatalf("%d fires read as blocked, want the one that was", report.Blocked)
	}
	if !report.Fires[0].At.Equal(firedAt) {
		t.Fatalf("the first fire reads as fired at %v, want %v", report.Fires[0].At, firedAt)
	}
}

func TestRulesFiredCreatesNoFile(t *testing.T) {
	dir := recordedFires(t)
	before, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("listing the log dir: %v", err)
	}
	var out, errOut bytes.Buffer
	if code := rulesFiredVerb(nil, &out, &errOut); code != exitOK {
		t.Fatalf("rules fired exited %d: %s", code, errOut.String())
	}
	after, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("listing the log dir: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("the read path left %d entries where there were %d", len(after), len(before))
	}
	if !strings.HasPrefix(out.String(), "2 fires recorded, 1 blocked\n") {
		t.Fatalf("rules fired printed:\n%s", out.String())
	}
}

func TestRulesFiredOnAnEmptyLogDirCreatesNothing(t *testing.T) {
	isolatedHomeAndProject(t)
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	var out, errOut bytes.Buffer
	if code := rulesFiredVerb(nil, &out, &errOut); code != exitOK {
		t.Fatalf("rules fired exited %d: %s", code, errOut.String())
	}
	if out.String() != "0 fires recorded, 0 blocked\n" {
		t.Fatalf("rules fired with nothing recorded printed %q", out.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the read path created %s", dir)
	}
}

func TestRulesFiredPrintsNoSecretCarriedByTheRecord(t *testing.T) {
	dir := recordedFires(t)
	secrets := []string{"not-a-real-token-a", "not-a-real-account-a", "not-a-real-key-a"}
	line := `{"rule_id":"leaky","target":"internal/three.go","mode":"shadow","blocked":false,` +
		`"findings":0,"at":"2026-09-21T15:04:05Z","access_token":"` + secrets[0] +
		`","account_id":"` + secrets[1] + `","api_key":"` + secrets[2] + `"}` + "\n"
	path := filepath.Join(dir, "2026-09-21"+rulesFireSuffix)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	if err := os.WriteFile(path, append(body, line...), 0o644); err != nil {
		t.Fatalf("seeding the leaky record: %v", err)
	}

	for _, args := range [][]string{nil, {jsonFlag}} {
		var out, errOut bytes.Buffer
		if code := rulesFiredVerb(args, &out, &errOut); code != exitOK {
			t.Fatalf("rules fired %v exited %d: %s", args, code, errOut.String())
		}
		if !strings.Contains(out.String(), "leaky") {
			t.Fatalf("rules fired %v did not read the seeded row:\n%s", args, out.String())
		}
		for _, secret := range secrets {
			if strings.Contains(out.String(), secret) {
				t.Fatalf("rules fired %v printed %q", args, secret)
			}
		}
	}
}
