package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixtureModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module tofu\n\ngo 1.25\n"
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

func TestImportsFailsOnACommandImportingBench(t *testing.T) {
	dir := writeFixtureModule(t, map[string]string{
		"bench/cost/cost.go":  "package cost\n\nfunc Run() int { return 1 }\n",
		"cmd/tofu/main.go":    "package main\n\nimport \"tofu/bench/cost\"\n\nfunc main() { _ = cost.Run() }\n",
		"internal/sys/sys.go": "package sys\n\nfunc OS() string { return \"windows\" }\n",
	})

	out := &bytes.Buffer{}
	code := doctorImports(dir, out)
	if code != exitVerdict {
		t.Fatalf("exit = %d, want %d, output %q", code, exitVerdict, out.String())
	}
	printed := out.String()
	want := "violation: tofu/cmd/tofu imports tofu/bench/cost, and nothing under tofu/cmd imports tofu/bench"
	if !strings.Contains(printed, want) {
		t.Fatalf("output %q, want a line %q", printed, want)
	}
	if !strings.Contains(printed, "imports: 3 packages, 10 rules, 1 violations") {
		t.Fatalf("output %q, want the counted summary", printed)
	}
}

func TestImportsFailsOnJudgeImportingTurn(t *testing.T) {
	dir := writeFixtureModule(t, map[string]string{
		"internal/turn/turn.go":         "package turn\n\nfunc Step() int { return 1 }\n",
		"internal/judge/policy/pol.go":  "package policy\n\nimport \"tofu/internal/turn\"\n\nfunc Decide() int { return turn.Step() }\n",
		"internal/judge/jev/client.go":  "package jev\n\nimport \"tofu/internal/sys\"\n\nfunc Name() string { return sys.OS() }\n",
		"internal/sys/sys.go":           "package sys\n\nfunc OS() string { return \"windows\" }\n",
		"internal/transport/retry.go":   "package transport\n\nfunc Retries() int { return 3 }\n",
		"internal/judge/ledger/row.go":  "package ledger\n\nimport \"tofu/internal/transport\"\n\nfunc N() int { return transport.Retries() }\n",
		"internal/judge/state/state.go": "package state\n\nimport \"tofu/internal/judge/ledger\"\n\nfunc N() int { return ledger.N() }\n",
	})

	out := &bytes.Buffer{}
	code := doctorImports(dir, out)
	if code != exitVerdict {
		t.Fatalf("exit = %d, want %d, output %q", code, exitVerdict, out.String())
	}
	want := "violation: tofu/internal/judge/policy imports tofu/internal/turn, and under tofu/internal, " +
		"a package in tofu/internal/judge imports only " +
		"tofu/internal/judge, tofu/internal/sys, tofu/internal/konst, tofu/internal/transport"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output %q, want a line %q", out.String(), want)
	}
}

func TestImportsPassesOnATreeThatObeysTheRules(t *testing.T) {
	dir := writeFixtureModule(t, map[string]string{
		"bench/cost/cost.go":           "package cost\n\nimport \"tofu/internal/judge/jev\"\n\nfunc Run() string { return jev.Name() }\n",
		"cmd/tofu/main.go":             "package main\n\nimport \"tofu/internal/judge/jev\"\n\nfunc main() { _ = jev.Name() }\n",
		"internal/judge/jev/client.go": "package jev\n\nimport \"tofu/internal/sys\"\n\nfunc Name() string { return sys.OS() }\n",
		"internal/sys/sys.go":          "package sys\n\nfunc OS() string { return \"windows\" }\n",
	})

	out := &bytes.Buffer{}
	code := doctorImports(dir, out)
	if code != exitOK {
		t.Fatalf("exit = %d, want %d, output %q", code, exitOK, out.String())
	}
	if !strings.Contains(out.String(), "imports: 4 packages, 10 rules, 0 violations") {
		t.Fatalf("output %q, want the counted summary", out.String())
	}
}

func TestImportsNamesEveryRuleItChecked(t *testing.T) {
	dir := writeFixtureModule(t, map[string]string{
		"internal/sys/sys.go": "package sys\n\nfunc OS() string { return \"windows\" }\n",
	})

	out := &bytes.Buffer{}
	if code := doctorImports(dir, out); code != exitOK {
		t.Fatalf("exit = %d, output %q", code, out.String())
	}
	for _, rule := range importRules() {
		if !strings.Contains(out.String(), "rule: "+rule.String()+"\n") {
			t.Fatalf("output %q, want it to name the rule %q", out.String(), rule)
		}
	}
}

func TestImportsOnThisTreeFindsNothingButTheKnownCommandToBenchEdge(t *testing.T) {
	out := &bytes.Buffer{}
	code := doctorImports(filepath.Join("..", ".."), out)
	t.Log("\n" + out.String())
	for _, line := range strings.Split(out.String(), "\n") {
		if !strings.HasPrefix(line, "violation: ") {
			continue
		}
		if !strings.HasPrefix(line, "violation: tofu/cmd/tofu imports tofu/bench/") {
			t.Fatalf("an import violation outside the known cmd to bench edge: %q", line)
		}
	}
	if code != exitOK && code != exitVerdict {
		t.Fatalf("exit = %d, output %q", code, out.String())
	}
}

func TestImportsReportsAnUnreadableTree(t *testing.T) {
	out := &bytes.Buffer{}
	code := doctorImports(filepath.Join(t.TempDir(), "absent"), out)
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d, output %q", code, exitUsage, out.String())
	}
	if !strings.Contains(out.String(), "imports: unreadable:") {
		t.Fatalf("output %q, want it to say the tree could not be read", out.String())
	}
}
