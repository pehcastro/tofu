package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/sys"
	"tofu/internal/turn"
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

func doctorOutput(t *testing.T) string {
	t.Helper()
	out := &bytes.Buffer{}
	doctor(out, plain)
	return out.String()
}

func doctorLine(t *testing.T, out, want string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	t.Fatalf("no line carrying %q in:\n%s", want, out)
	return ""
}

func doctorFix(t *testing.T, out, what string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	for index, line := range lines {
		if strings.Contains(line, what) && index+1 < len(lines) {
			return strings.TrimSpace(lines[index+1])
		}
	}
	t.Fatalf("no line carrying %q in:\n%s", what, out)
	return ""
}

func doctorJSON(t *testing.T) doctorReport {
	t.Helper()
	out := &bytes.Buffer{}
	doctor(out, plain, "--json")
	var report doctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("tofu doctor --json does not parse: %v\n%s", err, out.String())
	}
	return report
}

func TestDoctorPutsTheVerdictOnTheFirstLine(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	first := strings.Split(doctorOutput(t), "\n")[0]
	if !strings.HasPrefix(first, "tofu ") {
		t.Fatalf("the first line does not name the binary: %q", first)
	}
	if !strings.HasSuffix(first, doctorNotReady.String()) {
		t.Fatalf("the first line does not end in the verdict: %q", first)
	}
}

func TestDoctorNamesTheMissingCredentialAndTheCommandThatFixesIt(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	out := &bytes.Buffer{}
	if code := doctor(out, plain); code != exitVerdict {
		t.Fatalf("exit = %d, want %d, output:\n%s", code, exitVerdict, out.String())
	}
	if fix := doctorFix(t, out.String(), noCredential); fix != "run tofu login "+wireSubscription {
		t.Fatalf("the blocker is followed by %q, not the command that fixes it", fix)
	}
}

func TestDoctorNamesTheMissingKeyAndTheCommandThatFixesIt(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), "")
	if fix := doctorFix(t, doctorOutput(t), noGateKey); fix != "run tofu login "+openRouterName {
		t.Fatalf("the blocker is followed by %q, not the command that fixes it", fix)
	}
}

func TestDoctorNamesWhereTheKeyCameFrom(t *testing.T) {
	t.Run("environment", func(t *testing.T) {
		chdirTemp(t)
		t.Setenv(envVarName(), fakeSecret("env"))
		line := doctorLine(t, doctorOutput(t), jevName)
		if !strings.Contains(line, envVarName()+" in the environment") {
			t.Fatalf("the jev line = %q", line)
		}
	})

	t.Run("dotenv", func(t *testing.T) {
		dir := chdirTemp(t)
		t.Setenv(envVarName(), "")
		body := envVarName() + "=" + fakeSecret("file") + "\n"
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(body), 0o600); err != nil {
			t.Fatalf("writing the fixture: %v", err)
		}
		line := doctorLine(t, doctorOutput(t), jevName)
		if !strings.Contains(line, "key from .env") {
			t.Fatalf("the jev line = %q", line)
		}
		if strings.Contains(line, mustAbs(t, dir)) {
			t.Fatalf("the jev line repeats the root: %q", line)
		}
	})
}

func TestDoctorPrintsTheRootAtMostOnce(t *testing.T) {
	dir := chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	writeRuleFixture(t, "mode: enforced\n")
	printed := doctorOutput(t)
	if count := strings.Count(printed, mustAbs(t, dir)); count != 1 {
		t.Fatalf("the root appears %d times, want once:\n%s", count, printed)
	}
}

func TestDoctorStaysUnderTwentyFiveLines(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	printed := doctorOutput(t)
	if lines := strings.Count(printed, "\n"); lines > 25 {
		t.Fatalf("tofu doctor printed %d lines:\n%s", lines, printed)
	}
}

func TestDoctorCollapsesPointsThatSayTheSameThing(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	printed := doctorOutput(t)
	points := len(doctorJSON(t).Rules)
	line := doctorLine(t, printed, "points, all shadow")
	if !strings.Contains(line, fmt.Sprintf("%d points, all shadow, thresholds from the rule", points)) {
		t.Fatalf("the rules line = %q, want it to name all %d points", line, points)
	}
	if strings.Count(printed, "thresholds from") != 1 {
		t.Fatalf("the thresholds are printed more than once:\n%s", printed)
	}
	if !strings.Contains(printed, runGatePoint+doctorGateDecides) {
		t.Fatalf("the collapsed line never names the point the gate uses:\n%s", printed)
	}
}

func TestDoctorJSONCarriesEveryPointTheTextCollapsed(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	report := doctorJSON(t)
	if len(report.Rules) < 6 {
		t.Fatalf("tofu doctor --json carries %d points, want the whole library", len(report.Rules))
	}
	if strings.Count(doctorOutput(t), "shadow") > 1 {
		t.Fatal("the text form did not collapse, so the json proves nothing")
	}
	for _, point := range report.Rules {
		if point.Point == "" || point.Mode == "" || point.File == "" || point.ThresholdsFrom == "" {
			t.Fatalf("a point lost a field the text collapsed: %+v", point)
		}
		if point.Schema == "" && (point.Thresholds.RiskAskAt == 0 || point.Thresholds.RiskDenyAt == 0) {
			t.Fatalf("a gate point lost its thresholds: %+v", point)
		}
		if point.Schema != "" && point.Thresholds != (doctorThresholds{}) {
			t.Fatalf("a point with its own schema was given the gate's thresholds: %+v", point)
		}
	}
	if report.Root == "" || report.Go == "" || report.Ledger == "" || report.SpendLimit == "" {
		t.Fatalf("the json dropped a field the text carries: %+v", report)
	}
}

func fixtureThresholds() string { return "thresholds from the rule" }

func writeRuleFixture(t *testing.T, extra string) {
	t.Helper()
	libraryDir, err := sys.LibraryDir()
	if err != nil {
		t.Fatalf("library dir: %v", err)
	}
	path := filepath.Join(libraryDir, "general", "rules", "tool_gate@1.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "name: tool_gate\ndomain: general\nkind: threshold\nrule_version: 1\nquestions: tool_gate\nquestions_version: 1\nsample_floor: 300\n" +
		"risk_question: risk\napproval_question: approval\nuser_requested_question: user_requested\nfrom_untrusted_question: from_untrusted\n" + extra
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing rule fixture: %v", err)
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
		"rule: tool_gate\nrule_version: 1\nquestions: tool_gate\nquestions_version: %d\n"+
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
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	row := ledger.Row{Point: "tool_gate", Questions: "tool_gate", Version: 1, Build: currentBuildFixture}
	if _, err := ledger.NewWriter(dir).Append(row); err != nil {
		t.Fatalf("writing ledger fixture: %v", err)
	}
}

func rulePointOf(t *testing.T, point string) doctorRule {
	t.Helper()
	for _, candidate := range doctorJSON(t).Rules {
		if candidate.Point == point {
			return candidate
		}
	}
	t.Fatalf("no point %s in tofu doctor --json", point)
	return doctorRule{}
}

func TestDoctorNamesEveryFallbackToShadowOnItsOwnLine(t *testing.T) {
	cases := []struct {
		name string
		lock func(t *testing.T)
		want string
	}{
		{
			name: "no lock",
			lock: func(*testing.T) {},
			want: "no lock file for tool_gate@1 at build " + currentBuildFixture,
		},
		{
			name: "build mismatch",
			lock: func(t *testing.T) { writeLockFixture(t, "typesafe/jev-1.12-20260901", 1, 500, 400) },
			want: "lock build typesafe/jev-1.12-20260901 does not match current build " + currentBuildFixture,
		},
		{
			name: "questions version mismatch",
			lock: func(t *testing.T) { writeLockFixture(t, currentBuildFixture, 2, 500, 400) },
			want: "lock questions_version 2 does not match current questions_version 1",
		},
		{
			name: "sample below the floor",
			lock: func(t *testing.T) { writeLockFixture(t, currentBuildFixture, 1, 50, 80) },
			want: "sample size 50 is below the floor 300, .local/research/calibration-design.md §3",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			chdirTemp(t)
			t.Setenv(envVarName(), fakeSecret("env"))
			writeRuleFixture(t, "mode: enforced\n")
			writeLedgerRowFixture(t)
			testCase.lock(t)
			printed := doctorOutput(t)
			if !strings.Contains(oneLine(printed), "tool_gate@1 shadow, "+testCase.want) {
				t.Fatalf("no fallback line carrying %q in:\n%s", testCase.want, printed)
			}
			if point := rulePointOf(t, "tool_gate@1"); point.Fallback != testCase.want || point.Declared != "enforced" {
				t.Fatalf("the json lost the fallback: %+v", point)
			}
		})
	}
}

func TestDoctorNamesARuleThatDeclaresNoMode(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	writeRuleFixture(t, "")
	line := doctorLine(t, doctorOutput(t), "tool_gate@1 shadow")
	if !strings.Contains(line, "the rule declares no mode") {
		t.Fatalf("the fallback line = %q", line)
	}
}

func TestDoctorReportsAPointEnforcedOnAValidLock(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	writeRuleFixture(t, "mode: enforced\n")
	writeLedgerRowFixture(t)
	writeLockFixture(t, currentBuildFixture, 1, 500, 400)
	line := doctorLine(t, doctorOutput(t), "tool_gate@1 enforced")
	if !strings.Contains(line, fixtureThresholds()) {
		t.Fatalf("the enforced line = %q, want it to name where the thresholds came from", line)
	}
	if point := rulePointOf(t, "tool_gate@1"); point.Fallback != "" || point.Mode != "enforced" {
		t.Fatalf("the json disagrees with the text: %+v", point)
	}
}

func TestDoctorReportsTheLibraryInTheBinaryWhenTheProjectHasNone(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	printed := doctorOutput(t)
	line := doctorLine(t, printed, "library")
	if !strings.Contains(line, "the one in the binary") {
		t.Fatalf("library line = %q, want it to name the library in the binary", line)
	}
	if rulePointOf(t, runGatePoint).Point != runGatePoint {
		t.Fatalf("doctor says nothing about %s, the point the gate decides through", runGatePoint)
	}
}

func TestDoctorReportsTheProjectLibraryWhenThereIsOne(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	writeRuleFixture(t, "mode: shadow\n")
	line := doctorLine(t, doctorOutput(t), "library")
	if !strings.Contains(line, "the project's own, 1 of ") {
		t.Fatalf("library line = %q, want it to count the project's own points", line)
	}
}

func TestDoctorStillReportsALibraryItCannotRead(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	libraryDir, err := sys.LibraryDir()
	if err != nil {
		t.Fatalf("library dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(libraryDir, "general", "rules", "tool_gate@1.yaml"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	line := doctorLine(t, doctorOutput(t), "library")
	if !strings.Contains(line, doctorUnreadable) {
		t.Fatalf("library line = %q, want an unreadable library to stay an error", line)
	}
	if report := doctorJSON(t); report.Library.Unreadable == "" || len(report.Rules) != 0 {
		t.Fatalf("the json lists points over a library it cannot read: %+v", report.Library)
	}
}

func TestDoctorRejectsAnUnknownArgument(t *testing.T) {
	out := &bytes.Buffer{}
	code := doctor(out, plain, "--credentials")
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d, output %q", code, exitUsage, out.String())
	}
	if !strings.Contains(out.String(), `unknown argument "--credentials"`) {
		t.Fatalf("output %q, want it to name the argument", out.String())
	}
}

func TestDoctorSendsTheImportsFlagToTheImportCheck(t *testing.T) {
	out := &bytes.Buffer{}
	doctor(out, plain, "--imports")
	if !strings.HasPrefix(out.String(), "rule: ") {
		t.Fatalf("output %q, want the import report rather than the environment report", out.String())
	}
}

func TestGateSourcePanicsOnAnUnknownSource(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected an unknown source to panic")
		}
	}()
	_ = gateSource(jev.Located{Name: envVarName(), Source: jev.Source(99)})
}

func TestDoctorNeverPrintsTheKeyValue(t *testing.T) {
	envValue := fakeSecret("env-leak-check")
	fileValue := fakeSecret("file-leak-check")

	t.Run("environment", func(t *testing.T) {
		chdirTemp(t)
		t.Setenv(envVarName(), envValue)
		if strings.Contains(doctorOutput(t), envValue) {
			t.Fatal("doctor printed the environment key value")
		}
	})

	t.Run("dotenv", func(t *testing.T) {
		dir := chdirTemp(t)
		t.Setenv(envVarName(), "")
		body := envVarName() + "=" + fileValue + "\n"
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(body), 0o600); err != nil {
			t.Fatalf("writing the fixture: %v", err)
		}
		out := &bytes.Buffer{}
		doctor(out, plain, "--json")
		if strings.Contains(doctorOutput(t)+out.String(), fileValue) {
			t.Fatal("doctor printed the .env key value")
		}
	})
}

func TestDoctorSaysWhichWireSpendsMoney(t *testing.T) {
	chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("env"))
	report := doctorJSON(t)
	if len(report.Wires) != len(runWires()) {
		t.Fatalf("the json lists %d wires, the runner offers %d", len(report.Wires), len(runWires()))
	}
	for _, wire := range report.Wires {
		if _, err := parseRunArgs([]string{"--dir", ".", "--wire", wire.Name, "a task"}); err != nil {
			t.Fatalf("tofu run refuses --wire %s, so doctor describes a wire nobody can pick: %v", wire.Name, err)
		}
		saysMoney := strings.HasPrefix(wire.Spend, "money")
		if saysMoney != (wireSpend(wire.Name) == turn.SpendAPIKey) {
			t.Fatalf("doctor reads %q for --wire %s, whose turn row records spend %s", wire.Spend, wire.Name, wireSpend(wire.Name))
		}
	}
	line := doctorLine(t, doctorOutput(t), "wires")
	for _, wire := range report.Wires {
		if !strings.Contains(line, wire.Name) {
			t.Fatalf("the collapsed wire line %q drops %s", line, wire.Name)
		}
	}
}
