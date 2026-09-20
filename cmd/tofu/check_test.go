package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/state"
)

func toolGatePolicyPath(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "catalog", "policy", runGatePoint+".yaml"))
	if err != nil {
		t.Fatalf("resolving the shipped policy path: %v", err)
	}
	return abs
}

const askReply = `{"model":"typesafe/jev-1.13-20260917","provider":"TypeSafe","id":"gen-stub-check",` +
	`"answers":{` +
	`"risk":{"type":"score","score":3,"probabilities":{"0":0,"1":0,"2":0,"3":1},"confidence":0.9},` +
	`"approval":{"type":"noul","noul":0.9},` +
	`"user_requested":{"type":"noul","noul":0.95},` +
	`"from_untrusted":{"type":"noul","noul":0.02}` +
	`},"usage":{"input_tokens":10,"output_tokens":2,"cost":0.00002}}`

func TestCheckWritesARowWithAVerdict(t *testing.T) {
	policyPath := toolGatePolicyPath(t)
	t.Chdir(t.TempDir())
	client, err := jev.NewClient(jev.Config{Wire: &stubWire{reply: askReply}})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	row, err := runCheck(context.Background(), client, policyPath, "git push --force origin main")
	if err != nil {
		t.Fatalf("runCheck: %v", err)
	}
	if row.ID == "" {
		t.Fatal("the row carries no id")
	}
	if row.Verdict != ledger.VerdictAsk {
		t.Fatalf("verdict = %q, want ask (risk 3 relaxed by user_requested)", row.Verdict)
	}
	if row.Point != "tool_gate" {
		t.Fatalf("point = %q, want tool_gate", row.Point)
	}

	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger.Dir: %v", err)
	}
	found, ok, err := ledger.NewReader(dir).ByID(row.ID)
	if err != nil {
		t.Fatalf("reading the row back: %v", err)
	}
	if !ok {
		t.Fatalf("row %q is not in the ledger tofu why reads", row.ID)
	}
	if found.Verdict != ledger.VerdictAsk {
		t.Fatalf("the row read back carries verdict %q", found.Verdict)
	}
}

func TestCheckRowCarriesTheStateItWasDecidedOnAndTheTargetItNamed(t *testing.T) {
	policyPath := toolGatePolicyPath(t)
	project := t.TempDir()
	t.Chdir(project)
	client, err := jev.NewClient(jev.Config{Wire: &stubWire{reply: askReply}})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	row, err := runCheck(context.Background(), client, policyPath, "rm -f notes.md")
	if err != nil {
		t.Fatalf("runCheck: %v", err)
	}
	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger.Dir: %v", err)
	}
	found, ok, err := ledger.NewReader(dir).ByID(row.ID)
	if err != nil {
		t.Fatalf("reading the row back: %v", err)
	}
	if !ok {
		t.Fatalf("row %q is not in the ledger", row.ID)
	}
	var decoded struct {
		Context struct {
			Targets state.WriteTargets `json:"write_targets"`
		} `json:"context"`
	}
	if err := json.Unmarshal(found.State, &decoded); err != nil {
		t.Fatalf("the row carries no state tofu why can render: %v", err)
	}
	targets := decoded.Context.Targets
	if targets.Determination != state.TargetsResolved || len(targets.Targets) != 1 {
		t.Fatalf("write_targets = %+v, want one resolved target", targets)
	}
	if got := targets.Targets[0]; got.Location != state.LocationInsideProject || !strings.HasSuffix(got.Path, "/notes.md") {
		t.Fatalf("target = %+v, want notes.md inside the project", got)
	}
}

func TestCheckTwoCallsWriteTwoRows(t *testing.T) {
	policyPath := toolGatePolicyPath(t)
	t.Chdir(t.TempDir())
	client, err := jev.NewClient(jev.Config{Wire: &stubWire{reply: askReply}})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	first, err := runCheck(context.Background(), client, policyPath, "git push --force origin main")
	if err != nil {
		t.Fatalf("first runCheck: %v", err)
	}
	second, err := runCheck(context.Background(), client, policyPath, "ls -la")
	if err != nil {
		t.Fatalf("second runCheck: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("both checks produced the same row id %q", first.ID)
	}

	dir, err := ledger.Dir()
	if err != nil {
		t.Fatalf("ledger.Dir: %v", err)
	}
	var rows []ledger.Row
	if _, err := ledger.NewReader(dir).Each(ledger.Filter{}, func(row ledger.Row) error {
		rows = append(rows, row)
		return nil
	}); err != nil {
		t.Fatalf("reading the ledger: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func enforcedToolGatePolicyPath(t *testing.T) string {
	t.Helper()
	shipped, err := os.ReadFile(toolGatePolicyPath(t))
	if err != nil {
		t.Fatalf("reading the shipped policy: %v", err)
	}
	enforced := strings.Replace(string(shipped), "mode: shadow", "mode: enforced", 1)
	path := filepath.Join(t.TempDir(), "tool_gate@1.yaml")
	if err := os.WriteFile(path, []byte(enforced), 0o644); err != nil {
		t.Fatalf("writing the enforced policy fixture: %v", err)
	}
	return path
}

func TestCheckRowRecordsShadowWhateverThePolicyDeclares(t *testing.T) {
	policyPath := enforcedToolGatePolicyPath(t)
	t.Chdir(t.TempDir())
	client, err := jev.NewClient(jev.Config{Wire: &stubWire{reply: askReply}})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	row, err := runCheck(context.Background(), client, policyPath, "git push --force origin main")
	if err != nil {
		t.Fatalf("runCheck: %v", err)
	}
	if row.Mode() != ledger.ModeShadow {
		t.Fatalf("mode = %v, want shadow: check decides and never blocks, whatever the policy declares", row.Mode())
	}
}

type capturingWire struct {
	reply string
	body  []byte
}

func (w *capturingWire) Caps() jev.WireCaps {
	return jev.WireCaps{MaxRequestBytes: 90000, MaxChoiceOptions: 255, MaxScoreLevels: 10, ReturnsConfidence: true}
}

func (w *capturingWire) Model() string { return "~typesafe/jev-latest" }

func (w *capturingWire) Post(_ context.Context, body []byte) (jev.Raw, error) {
	w.body = body
	return jev.Raw{Body: []byte(w.reply), RequestID: "req-stub", Attempts: 1}, nil
}

func TestCheckSendsExactlyWhatTheStateBuilderProduces(t *testing.T) {
	policyPath := toolGatePolicyPath(t)
	t.Chdir(t.TempDir())
	wire := &capturingWire{reply: askReply}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	if _, err := runCheck(context.Background(), client, policyPath, "git push --force origin main"); err != nil {
		t.Fatalf("runCheck: %v", err)
	}

	want, _, err := state.BuildToolGateV3(state.ToolGateInput{
		Agent:      "owner-shell",
		Tool:       "bash",
		Input:      map[string]any{"command": "git push --force origin main"},
		Cwd:        cwd,
		ProjectDir: cwd,
	})
	if err != nil {
		t.Fatalf("state.BuildToolGateV3: %v", err)
	}

	var envelope struct {
		State json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(wire.body, &envelope); err != nil {
		t.Fatalf("decoding the posted request: %v", err)
	}
	if !bytes.Equal(envelope.State, want) {
		t.Fatalf("state sent = %s, want %s", envelope.State, want)
	}
}

func TestParseCheckArgsQuiet(t *testing.T) {
	opts, err := parseCheckArgs([]string{"--quiet", "rm -rf build"})
	if err != nil {
		t.Fatalf("parseCheckArgs: %v", err)
	}
	if !opts.quiet || opts.command != "rm -rf build" {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestParseCheckArgsRejectsTwoCommands(t *testing.T) {
	if _, err := parseCheckArgs([]string{"ls", "-la"}); err == nil {
		t.Fatal("parseCheckArgs accepted two positional arguments")
	}
}

func TestParseCheckArgsRejectsNoCommand(t *testing.T) {
	if _, err := parseCheckArgs([]string{"--quiet"}); err == nil {
		t.Fatal("parseCheckArgs accepted a run with no command")
	}
}

func TestCheckVerbNeedsTheKey(t *testing.T) {
	t.Setenv("OPENROUTER_KEY", "")
	t.Chdir(t.TempDir())
	var out, errOut bytes.Buffer
	code := checkVerb([]string{"ls -la"}, &out, &errOut)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "OPENROUTER_KEY") {
		t.Fatalf("stderr does not name the variable: %s", errOut.String())
	}
}
