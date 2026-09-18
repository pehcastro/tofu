package policy

import (
	"path/filepath"
	"testing"
)

func TestLoadLockParsesEveryField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool_gate@1.lock")
	writeFile(t, path, ""+
		"policy: tool_gate\n"+
		"policy_version: 1\n"+
		"questions: tool_gate\n"+
		"questions_version: 1\n"+
		"build: typesafe/jev-1.13-20260917\n"+
		"fitted_at: 2026-09-18T00:00:00Z\n"+
		"verified_at: 2026-09-18T01:00:00Z\n"+
		"n_fit: 500\n"+
		"n_verify: 400\n")
	lock, err := LoadLock(path)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if lock.Policy != "tool_gate" || lock.PolicyVersion != 1 {
		t.Fatalf("policy = %q version = %d", lock.Policy, lock.PolicyVersion)
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

func TestLoadLockRejectsAnUnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.lock")
	writeFile(t, path, "policy: tool_gate\nnot_a_field: 1\n")
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
	pol := fixturePolicy()
	got := LockPath(filepath.Join("tmp", "calibration"), pol)
	want := filepath.Join("tmp", "calibration", "tool_gate@1.lock")
	if got != want {
		t.Fatalf("LockPath = %q, want %q", got, want)
	}
}
