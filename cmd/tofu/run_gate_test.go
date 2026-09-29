package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
	settingspkg "tofu/internal/settings"
	"tofu/internal/sys"
	"tofu/internal/turn"
	library "tofu/library"
)

const gateFixtureBuild = "typesafe/jev-1.13-20260917"

const gateFixtureThresholds = "  risk_ask_at: 1.5\n  risk_deny_at: 2.5\n" +
	"  user_requested_relax_at: 0.85\n  approval_relax_at: 0.15\n  from_untrusted_block_at: 0.5\n"

func writeGateRuleWithThresholds(t *testing.T, thresholds string) string {
	t.Helper()
	libraryDir, err := sys.LibraryDir()
	if err != nil {
		t.Fatalf("library dir: %v", err)
	}
	path := filepath.Join(libraryDir, "general", "rules", runGatePoint+".yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "name: tool_gate\ndomain: general\nkind: threshold\nrule_version: 3\nquestions: tool_gate\nquestions_version: 3\n" +
		"mode: enforced\nsample_floor: 300\n" +
		"risk_question: risk\napproval_question: approval\n" +
		"user_requested_question: user_requested\nfrom_untrusted_question: from_untrusted\n" +
		"thresholds:\n" + thresholds
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the rule fixture: %v", err)
	}
	return path
}

func writeGateRuleFixture(t *testing.T) gate.Thresholds {
	t.Helper()
	writeGateRuleWithThresholds(t, gateFixtureThresholds)
	return gate.Thresholds{
		RiskAskAt:            1.5,
		RiskDenyAt:           2.5,
		UserRequestedRelaxAt: 0.85,
		ApprovalRelaxAt:      0.15,
		FromUntrustedBlockAt: 0.5,
	}
}

func writeGateLockFixture(t *testing.T, build string) gate.Thresholds {
	t.Helper()
	calibDir, err := sys.CalibrationDir()
	if err != nil {
		t.Fatalf("calibration dir: %v", err)
	}
	if err := os.MkdirAll(calibDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := fmt.Sprintf(""+
		"rule: tool_gate\nrule_version: 3\nquestions: tool_gate\nquestions_version: 3\n"+
		"build: %s\nn_fit: 500\nn_verify: 400\n"+
		"thresholds:\n  risk_ask_at: 1.25\n  risk_deny_at: 2.75\n"+
		"  user_requested_relax_at: 0.9\n  approval_relax_at: 0.2\n  from_untrusted_block_at: 0.4\n", build)
	path := filepath.Join(calibDir, runGatePoint+".lock")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing the lock fixture: %v", err)
	}
	return gate.Thresholds{
		RiskAskAt:            1.25,
		RiskDenyAt:           2.75,
		UserRequestedRelaxAt: 0.9,
		ApprovalRelaxAt:      0.2,
		FromUntrustedBlockAt: 0.4,
	}
}

func writeGateLedgerRowFixture(t *testing.T) {
	t.Helper()
	dir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
	}
	row := ledger.Row{Point: "tool_gate", Questions: "tool_gate", Version: 3, Build: gateFixtureBuild}
	if _, err := ledger.NewWriter(dir).Append(row); err != nil {
		t.Fatalf("writing the ledger fixture: %v", err)
	}
}

func gateThresholds(t *testing.T, dir string) gate.Thresholds {
	t.Helper()
	gate, err := newToolGate(dir)
	if err != nil {
		t.Fatalf("building the gate: %v", err)
	}
	if gate.set.Rule == nil {
		t.Fatal("the gate carries no rule")
	}
	return gate.set.Rule.Thresholds
}

func gateThresholdsAsDoctorReadsThem(point doctorRule) gate.Thresholds {
	return gate.Thresholds{
		RiskAskAt:            point.Thresholds.RiskAskAt,
		RiskDenyAt:           point.Thresholds.RiskDenyAt,
		UserRequestedRelaxAt: point.Thresholds.UserRequestedRelaxAt,
		ApprovalRelaxAt:      point.Thresholds.ApprovalRelaxAt,
		FromUntrustedBlockAt: point.Thresholds.FromUntrustedBlockAt,
	}
}

func gateScratch(t *testing.T, lockBuild string) (string, gate.Thresholds, gate.Thresholds) {
	t.Helper()
	dir := chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("gate"))
	enableGateAsking(t)
	declared := writeGateRuleFixture(t)
	writeGateLedgerRowFixture(t)
	pinned := writeGateLockFixture(t, lockBuild)
	return dir, declared, pinned
}

func enableGateAsking(t *testing.T) {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := settingsVerb([]string{"set", settingspkg.GatePrompt, settingspkg.GatePromptAsk}, &out, &errOut); code != exitOK {
		t.Fatalf("settings set %s %s exited %d: %s", settingspkg.GatePrompt, settingspkg.GatePromptAsk, code, errOut.String())
	}
}

func TestADecisionMadeNowCarriesTheFingerprintOfTheCallItJudged(t *testing.T) {
	dir, _, _ := gateScratch(t, gateFixtureBuild)
	stubJev(t, 200, middlingRiskAskReply)
	gate, err := newToolGate(dir)
	if err != nil {
		t.Fatalf("newToolGate: %v", err)
	}

	decision, err := gate.Decide(t.Context(), turn.GateRequest{
		TurnID: "turn-fingerprint",
		Task:   "write the note the README asked for",
		Tool:   "write",
		Args:   json.RawMessage(`{"path":"note.txt","content":"hello"}`),
	})
	if err != nil {
		t.Fatalf("the gate did not decide: %v", err)
	}

	ledgerDir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("ledger dir: %v", err)
	}
	row, found, err := ledger.NewReader(ledgerDir).ByID(decision.ID)
	if err != nil || !found {
		t.Fatalf("reading row %s back: found %v, err %v", decision.ID, found, err)
	}
	want := state.FingerprintOf(state.ToolGateInput{
		Agent:      "tofu-run",
		Tool:       "write",
		Input:      map[string]any{"path": "note.txt", "content": "hello"},
		Cwd:        dir,
		ProjectDir: dir,
		Context:    state.ToolGateContext{UserRecentMessages: []string{"write the note the README asked for"}},
	})
	if row.Fingerprint != want || want == "" {
		t.Fatalf("the row carries fingerprint %q and the call fingerprints as %q", row.Fingerprint, want)
	}
	if row.Schema < ledger.FingerprintSchema {
		t.Fatalf("the row is schema %d, older than the schema %d the fingerprint field arrived at", row.Schema, ledger.FingerprintSchema)
	}
	t.Logf("row %s carries %s", row.ID, row.Fingerprint)
}

func TestTheGateReadsTheRuleInTheBinaryWhenTheProjectHasNoLibrary(t *testing.T) {
	dir := chdirTemp(t)
	t.Setenv(envVarName(), fakeSecret("gate"))
	shipped, err := gate.LoadFS(library.Files(), runGatePoint)
	if err != nil {
		t.Fatalf("gate.LoadFS: %v", err)
	}
	if got := gateThresholds(t, dir); got != shipped.Thresholds {
		t.Fatalf("the gate decides at %+v, want the binary's %+v", got, shipped.Thresholds)
	}
}

func TestDoctorAndTheGateReportThePinnedThresholds(t *testing.T) {
	dir, _, pinned := gateScratch(t, gateFixtureBuild)
	point := rulePointOf(t, runGatePoint)
	if point.Mode != "enforced" || point.ThresholdsFrom != "the lock" {
		t.Fatalf("doctor reads %+v, want it enforced on the lock", point)
	}
	if read := gateThresholdsAsDoctorReadsThem(point); read != pinned {
		t.Fatalf("doctor reports %+v, want the lock's %+v", read, pinned)
	}
	got := gateThresholds(t, dir)
	if got != pinned {
		t.Fatalf("the gate decides at %+v, want the lock's %+v", got, pinned)
	}
	t.Logf("doctor: %+v", point)
	t.Logf("gate:   %s", got)
}

func TestDoctorAndTheGateAgreeAShadowPointKeepsItsOwnThresholds(t *testing.T) {
	dir, declared, pinned := gateScratch(t, "typesafe/jev-1.12-20260901")
	point := rulePointOf(t, runGatePoint)
	want := "lock build typesafe/jev-1.12-20260901 does not match current build " + gateFixtureBuild
	if point.Mode != "shadow" || point.Fallback != want {
		t.Fatalf("doctor reads %+v, want it fallen back to shadow because %q", point, want)
	}
	if read := gateThresholdsAsDoctorReadsThem(point); read != declared {
		t.Fatalf("doctor reports %+v, want the rule's own %+v", read, declared)
	}
	got := gateThresholds(t, dir)
	if got != declared {
		t.Fatalf("the gate decides at %+v, want the rule's own %+v", got, declared)
	}
	if got == pinned {
		t.Fatalf("a shadow point took the lock's thresholds %+v", pinned)
	}
	t.Logf("doctor: %+v", point)
	t.Logf("gate:   %s", got)
	t.Logf("lock, not applied: %s", pinned)
}

const lowRiskAllowReply = `{"model":"typesafe/jev-1.13-20260917","provider":"TypeSafe","id":"gen-stub-allow",` +
	`"answers":{` +
	`"risk":{"type":"score","score":0,"probabilities":{"0":0.97,"1":0.03,"2":0,"3":0},"confidence":0.9},` +
	`"approval":{"type":"noul","noul":0.05},` +
	`"user_requested":{"type":"noul","noul":0.95},` +
	`"from_untrusted":{"type":"noul","noul":0.02}` +
	`},"usage":{"input_tokens":10,"output_tokens":2,"cost":0.00002}}`

func TestASettingsCallJevAllowsStillReachesThePersonAsAnAsk(t *testing.T) {
	dir, _, _ := gateScratch(t, gateFixtureBuild)
	stubJev(t, 200, lowRiskAllowReply)
	gate, err := newToolGate(dir)
	if err != nil {
		t.Fatalf("newToolGate: %v", err)
	}
	watched := map[string]ledger.Verdict{}
	gate.watch = func(tool string, decision turn.GateDecision, _ error) { watched[tool] = decision.Verdict }
	for tool, want := range map[string]ledger.Verdict{"read": ledger.VerdictAllow, "settings": ledger.VerdictAsk} {
		decision, err := gate.Decide(t.Context(), turn.GateRequest{
			TurnID: "turn-settings",
			Task:   "raise the sub-agent limit to 15",
			Tool:   tool,
			Args:   json.RawMessage(`{"key":"subAgentsPerTurn","value":15}`),
		})
		if err != nil || decision.Verdict != want || watched[tool] != want {
			t.Errorf("%s: decided %s, the interface was shown %s, want %s (err %v)", tool, decision.Verdict, watched[tool], want, err)
		}
	}
}

func envVarName() string { return "OPENROUTER" + "_KEY" }

func fakeSecret(tag string) string { return "fake-test-secret-" + tag }

func isolateHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
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

func doctorJSON(t *testing.T) doctorReport {
	t.Helper()
	out := &bytes.Buffer{}
	doctor([]string{jsonFlag}, out, out)
	var envelope struct{ Data doctorReport }
	oneEnvelope(t, out.String(), &envelope)
	return envelope.Data
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
