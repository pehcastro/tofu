package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"boji/internal/judge/jev"
	"boji/internal/judge/ledger"
	"boji/internal/sys"
)

func envVarName() string { return "OPENROUTER" + "_KEY" }

func fakeSecret(tag string) string { return "fake-test-secret-" + tag }

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolving %s: %v", path, err)
	}
	return abs
}

func chdirTemp(t *testing.T) string {
	t.Helper()
	isolateHome(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	return dir
}

func keyLine(t *testing.T, out string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("doctor output too short: %q", out)
	}
	return lines[2]
}

func TestDoctorNamesTheEnvironment(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	out := &bytes.Buffer{}
	code := doctor(out)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d, output %q", code, exitOK, out.String())
	}
	line := keyLine(t, out.String())
	if !strings.Contains(line, "environment") {
		t.Fatalf("key line = %q, want it to name the environment", line)
	}
}

func TestDoctorNamesTheDotEnvFile(t *testing.T) {
	dir := chdirTemp(t)
	t.Setenv(envVarName(), "")
	path := filepath.Join(dir, ".env")
	body := envVarName() + "=" + fakeSecret("file") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	out := &bytes.Buffer{}
	code := doctor(out)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d, output %q", code, exitOK, out.String())
	}
	line := keyLine(t, out.String())
	if !strings.Contains(line, ".env") {
		t.Fatalf("key line = %q, want it to name .env", line)
	}
	want := mustAbs(t, path)
	if !strings.Contains(line, want) {
		t.Fatalf("key line = %q, want it to name %s", line, want)
	}
}

func TestDoctorSaysTheKeyIsMissing(t *testing.T) {
	dir := chdirTemp(t)
	t.Setenv(envVarName(), "")
	out := &bytes.Buffer{}
	code := doctor(out)
	if code != exitVerdict {
		t.Fatalf("exit = %d, want %d, output %q", code, exitVerdict, out.String())
	}
	line := keyLine(t, out.String())
	if !strings.Contains(line, "missing") {
		t.Fatalf("key line = %q, want it to say the key is missing", line)
	}
	want := mustAbs(t, filepath.Join(dir, ".env"))
	if !strings.Contains(line, want) {
		t.Fatalf("key line = %q, want it to say where it looks (%s)", line, want)
	}
}

func policyLine(t *testing.T, out string) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "policy tool_gate@1:") {
		t.Fatalf("doctor output has no policy line, last line %q, full output %q", last, out)
	}
	return last
}

func writePolicyFixture(t *testing.T, extra string) {
	t.Helper()
	catalogDir, err := sys.CatalogDir()
	if err != nil {
		t.Fatalf("catalog dir: %v", err)
	}
	path := filepath.Join(catalogDir, "policy", "tool_gate@1.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "name: tool_gate\npolicy_version: 1\nquestions: tool_gate\nquestions_version: 1\nsample_floor: 300\n" + extra
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing policy fixture: %v", err)
	}
}

func writeLockFixture(t *testing.T, build string, questionsVersion, nFit, nVerify int) {
	t.Helper()
	calibDir, err := sys.CalibrationDir()
	if err != nil {
		t.Fatalf("calibration dir: %v", err)
	}
	if err := os.MkdirAll(calibDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := fmt.Sprintf(""+
		"policy: tool_gate\npolicy_version: 1\nquestions: tool_gate\nquestions_version: %d\n"+
		"build: %s\nn_fit: %d\nn_verify: %d\n",
		questionsVersion, build, nFit, nVerify)
	path := filepath.Join(calibDir, "tool_gate@1.lock")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing lock fixture: %v", err)
	}
}

const currentBuildFixture = "typesafe/jev-1.13-20260917"

func writeLedgerRowFixture(t *testing.T) {
	t.Helper()
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger dir: %v", err)
	}
	row := ledger.Row{Point: "tool_gate", Questions: "tool_gate", Version: 1, Build: currentBuildFixture}
	if _, err := ledger.NewWriter(dir).Append(row); err != nil {
		t.Fatalf("writing ledger fixture: %v", err)
	}
}

func TestDoctorListsAPointEnforcedWithNoLockFallingBackToShadow(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	writePolicyFixture(t, "mode: enforced\n")
	writeLedgerRowFixture(t)
	out := &bytes.Buffer{}
	doctor(out)
	line := policyLine(t, out.String())
	want := "policy tool_gate@1: shadow, no lock file for tool_gate@1 at build " + currentBuildFixture
	if line != want {
		t.Fatalf("policy line = %q, want %q", line, want)
	}
}

func TestDoctorNamesABuildMismatch(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	writePolicyFixture(t, "mode: enforced\n")
	writeLedgerRowFixture(t)
	writeLockFixture(t, "typesafe/jev-1.12-20260901", 1, 500, 400)
	out := &bytes.Buffer{}
	doctor(out)
	line := policyLine(t, out.String())
	want := "policy tool_gate@1: shadow, lock build typesafe/jev-1.12-20260901 does not match current build " + currentBuildFixture
	if line != want {
		t.Fatalf("policy line = %q, want %q", line, want)
	}
}

func TestDoctorNamesAQuestionsVersionMismatch(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	writePolicyFixture(t, "mode: enforced\n")
	writeLedgerRowFixture(t)
	writeLockFixture(t, currentBuildFixture, 2, 500, 400)
	out := &bytes.Buffer{}
	doctor(out)
	line := policyLine(t, out.String())
	want := "policy tool_gate@1: shadow, lock questions_version 2 does not match current questions_version 1"
	if line != want {
		t.Fatalf("policy line = %q, want %q", line, want)
	}
}

func TestDoctorNamesASampleSizeBelowTheFloor(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	writePolicyFixture(t, "mode: enforced\n")
	writeLedgerRowFixture(t)
	writeLockFixture(t, currentBuildFixture, 1, 50, 80)
	out := &bytes.Buffer{}
	doctor(out)
	line := policyLine(t, out.String())
	want := "policy tool_gate@1: shadow, sample size 50 is below the floor 300, .local/research/calibration-design.md §3"
	if line != want {
		t.Fatalf("policy line = %q, want %q", line, want)
	}
}

func TestDoctorNamesAMissingMode(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	writePolicyFixture(t, "")
	out := &bytes.Buffer{}
	doctor(out)
	line := policyLine(t, out.String())
	want := "policy tool_gate@1: shadow, the policy declares no mode"
	if line != want {
		t.Fatalf("policy line = %q, want %q", line, want)
	}
}

func TestDoctorReportsAPointEnforcedOnAValidLock(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	writePolicyFixture(t, "mode: enforced\n")
	writeLedgerRowFixture(t)
	writeLockFixture(t, currentBuildFixture, 1, 500, 400)
	out := &bytes.Buffer{}
	doctor(out)
	line := policyLine(t, out.String())
	want := "policy tool_gate@1: enforced"
	if line != want {
		t.Fatalf("policy line = %q, want %q", line, want)
	}
}

func TestDoctorRejectsAnUnknownArgument(t *testing.T) {
	out := &bytes.Buffer{}
	code := doctor(out, "--credentials")
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d, output %q", code, exitUsage, out.String())
	}
	if !strings.Contains(out.String(), `unknown argument "--credentials"`) {
		t.Fatalf("output %q, want it to name the argument", out.String())
	}
}

func TestDoctorSendsTheImportsFlagToTheImportCheck(t *testing.T) {
	out := &bytes.Buffer{}
	doctor(out, "--imports")
	if !strings.HasPrefix(out.String(), "rule: ") {
		t.Fatalf("output %q, want the import report rather than the environment report", out.String())
	}
}

func TestKeyStatePanicsOnAnUnknownSource(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected an unknown source to panic")
		}
	}()
	_ = keyState(jev.Located{Name: envVarName(), Source: jev.Source(99)})
}

func TestDoctorNeverPrintsTheKeyValue(t *testing.T) {
	envValue := fakeSecret("env-leak-check")
	fileValue := fakeSecret("file-leak-check")

	t.Run("environment", func(t *testing.T) {
		chdirTemp(t)
		t.Setenv(envVarName(), envValue)
		out := &bytes.Buffer{}
		doctor(out)
		if strings.Contains(out.String(), envValue) {
			t.Fatalf("doctor printed the environment key value")
		}
	})

	t.Run("dotenv", func(t *testing.T) {
		dir := chdirTemp(t)
		t.Setenv(envVarName(), "")
		path := filepath.Join(dir, ".env")
		body := envVarName() + "=" + fileValue + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("writing the fixture: %v", err)
		}
		out := &bytes.Buffer{}
		doctor(out)
		if strings.Contains(out.String(), fileValue) {
			t.Fatalf("doctor printed the .env key value")
		}
	})
}

func TestDoctorReportsTheCredentialStateAndTheQuotaState(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	out := &bytes.Buffer{}
	if code := doctor(out); code != exitOK {
		t.Fatalf("exit = %d, output %q", code, out.String())
	}
	printed := out.String()
	if !strings.Contains(printed, "credential: none") {
		t.Fatalf("no credential line in %q", printed)
	}
	if !strings.Contains(printed, "spend limit: boji sets none") {
		t.Fatalf("no quota line in %q", printed)
	}
}

func TestDoctorPrintsTheCredentialLineAfterTheLedgerLineAndBeforeThePolicyLines(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	writePolicyFixture(t, "mode: shadow\n")
	out := &bytes.Buffer{}
	doctor(out)
	printed := out.String()
	ledgerAt := strings.Index(printed, "\nledger:")
	credentialAt := strings.Index(printed, "\ncredential:")
	policyAt := strings.Index(printed, "\npolicy ")
	if ledgerAt < 0 || credentialAt < 0 || policyAt < 0 {
		t.Fatalf("a line is missing from %q", printed)
	}
	if ledgerAt >= credentialAt || credentialAt >= policyAt {
		t.Fatalf("the order is ledger %d, credential %d, policy %d", ledgerAt, credentialAt, policyAt)
	}
}
