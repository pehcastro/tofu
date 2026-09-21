package gate

import (
	"path/filepath"
	"testing"
)

func lockText(thresholds string) string {
	return "rule: tool_gate\n" +
		"rule_version: 1\n" +
		"questions: tool_gate\n" +
		"questions_version: 1\n" +
		"build: typesafe/jev-1.13-20260917\n" +
		"fitted_at: 2026-09-18T00:00:00Z\n" +
		"verified_at: 2026-09-18T01:00:00Z\n" +
		"n_fit: 500\n" +
		"n_verify: 400\n" + thresholds
}

func TestLoadLockParsesEveryField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool_gate@1.lock")
	writeFile(t, path, lockText(""))
	lock, err := LoadLock(path)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if lock.Rule != "tool_gate" || lock.RuleVersion != 1 {
		t.Fatalf("rule = %q version = %d", lock.Rule, lock.RuleVersion)
	}
	if lock.Questions != "tool_gate" || lock.QuestionsVersion != 1 {
		t.Fatalf("questions = %q version = %d", lock.Questions, lock.QuestionsVersion)
	}
	if lock.Build != "typesafe/jev-1.13-20260917" {
		t.Fatalf("build = %q", lock.Build)
	}
	if lock.NFit != 500 || lock.NVerify != 400 {
		t.Fatalf("n_fit = %d n_verify = %d", lock.NFit, lock.NVerify)
	}
	if lock.FittedAt.IsZero() || lock.VerifiedAt.IsZero() {
		t.Fatalf("fitted_at or verified_at did not parse")
	}
}

func TestALockPinsTheThresholdsTheDecisionThenRunsAt(t *testing.T) {
	dir := t.TempDir()
	r := fixtureRule()
	r.Mode, r.ModeDeclared, r.SampleFloor = ModeEnforced, true, 100
	path := LockPath(dir, r)
	writeFile(t, path, lockText(""+
		"thresholds:\n"+
		"  risk_ask_at: 1.5\n"+
		"  risk_deny_at: 2.5\n"+
		"  user_requested_relax_at: 0.85\n"+
		"  approval_relax_at: 0.80\n"+
		"  from_untrusted_block_at: 0.5\n"))
	lock, err := LoadLock(path)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if !lock.PinsThresholds || lock.Thresholds.ApprovalRelaxAt != 0.80 {
		t.Fatalf("the lock pinned %+v, want approval_relax_at 0.80", lock.Thresholds)
	}
	current := Current{Build: lock.Build, QuestionsVersion: lock.QuestionsVersion, Known: true}
	lookup := LockLookup{Present: true, Lock: lock}
	enforced := Resolve(r, lookup, current)
	if enforced.Mode != ModeEnforced {
		t.Fatalf("mode = %s because %s, want enforced", enforced.Mode, enforced.Reason)
	}
	answers := neutralAnswers(r)
	answers[r.RiskQuestion] = scoreAnswerFixture(1.9)
	answers[r.ApprovalQuestion] = noulAnswerFixture(0.60)
	answers[r.UserRequestedQuestion] = noulAnswerFixture(0)
	before, _, err := Decide(answers, r)
	if err != nil {
		t.Fatalf("Decide before the lock: %v", err)
	}
	if !enforced.Pinned {
		t.Fatalf("an enforced resolution over a lock with thresholds is not pinned")
	}
	stale := Current{Build: "typesafe/jev-1.12-20260901", QuestionsVersion: lock.QuestionsVersion, Known: true}
	shadowed := Resolve(r, lookup, stale)
	if shadowed.Mode != ModeShadow {
		t.Fatalf("mode = %s over a lock fitted against another build, want shadow", shadowed.Mode)
	}
	if shadowed.Pinned || shadowed.Rule.Thresholds != r.Thresholds {
		t.Fatalf("a shadow point took the lock's thresholds: %+v", shadowed.Rule.Thresholds)
	}
	after, reason, err := Decide(answers, enforced.Rule)
	if err != nil {
		t.Fatalf("Decide under the lock: %v", err)
	}
	if before != VerdictAsk {
		t.Fatalf("the rule's own thresholds gave %s, want ask", before)
	}
	if after != VerdictAllow || reason.RelaxedBy != r.ApprovalQuestion {
		t.Fatalf("under the lock the verdict is %s relaxed by %q, want allow relaxed by approval", after, reason.RelaxedBy)
	}
}

func TestAPointWhoseLockFileIsNotOnDiskStaysInShadow(t *testing.T) {
	dir := t.TempDir()
	r := fixtureRule()
	r.Mode, r.ModeDeclared, r.SampleFloor = ModeEnforced, true, 100
	if _, err := LoadLock(LockPath(dir, r)); err == nil {
		t.Fatal("LoadLock found a lock in an empty directory")
	}
	resolution := Resolve(r, LockLookup{}, Current{Build: "typesafe/jev-1.13-20260917", Known: true})
	if resolution.Mode != ModeShadow {
		t.Fatalf("mode = %s, want shadow when no lock file backs the point", resolution.Mode)
	}
	if resolution.Reason != "no lock file for tool_gate@1 at build typesafe/jev-1.13-20260917" {
		t.Fatalf("reason = %q", resolution.Reason)
	}
}

func TestLoadLockRejectsAnUnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.lock")
	writeFile(t, path, "rule: tool_gate\nnot_a_field: 1\n")
	if _, err := LoadLock(path); err == nil {
		t.Fatal("LoadLock accepted an unknown field")
	}
}

func TestLoadLockRejectsAMissingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadLock(filepath.Join(dir, "missing.lock")); err == nil {
		t.Fatal("LoadLock accepted a missing file")
	}
}

func TestLockPathNamesThePointAndVersion(t *testing.T) {
	got := LockPath(filepath.Join("tmp", "calibration"), fixtureRule())
	want := filepath.Join("tmp", "calibration", "tool_gate@1.lock")
	if got != want {
		t.Fatalf("LockPath = %q, want %q", got, want)
	}
}
