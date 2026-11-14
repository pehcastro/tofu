package doctor

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/apps/cli/output"
)

func everyRuleMatched(files map[string]string) map[string]string {
	tree := map[string]string{
		"bench/suites/judge/cost/cost.go":       "package cost\n\nfunc Run() int { return 1 }\n",
		"internal/dummy/dummy.go":               "package dummy\n",
		"apps/cli/main.go":                      "package main\n\nfunc main() {}\n",
		"apps/cli/wiring/wiring.go":             "package wiring\n",
		"apps/cli/verbs/doctor/doctor.go":       "package doctor\n",
		"library/embed.go":                      "package library\n",
		"internal/judge/questions/questions.go": "package questions\n\nimport \"tofu/internal/platform/fs\"\n\nfunc Name() string { return fs.OS() }\n",
		"internal/platform/fs/os.go":            "package fs\n\nfunc OS() string { return \"windows\" }\n",
		"internal/konst/konst.go":               "package konst\n",
		"internal/platform/safe/safe.go":        "package safe\n",
	}
	for _, platform := range []string{"fs", "project", "clipboard", "console", "scratch", "credential", "profile", "build", "outside"} {
		tree["internal/platform/"+platform+"/"+platform+".go"] = "package " + platform + "\n"
	}
	maps.Copy(tree, files)
	return tree
}

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
	dir := writeFixtureModule(t, everyRuleMatched(map[string]string{
		"apps/cli/main.go": "package main\n\nimport \"tofu/bench/suites/judge/cost\"\n\nfunc main() { _ = cost.Run() }\n",
	}))

	out := &bytes.Buffer{}
	code := Imports(dir, false, out, out)
	if code != output.ExitVerdict {
		t.Fatalf("exit = %d, want %d, output %q", code, output.ExitVerdict, out.String())
	}
	for _, want := range []string{
		"Imports · 18 packages · 19 rules",
		"✗ 1 violations\n",
		"  ✗ tofu/apps                          never tofu/bench\n",
		"violations\n  ✗ tofu/apps/cli  imports tofu/bench/suites/judge/cost\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output\n%s\nwant a line %q", out.String(), want)
		}
	}
}

func TestImportsFailsOnJudgeImportingTurn(t *testing.T) {
	dir := writeFixtureModule(t, everyRuleMatched(map[string]string{
		"internal/agent/loop/loop.go":        "package loop\n\nfunc Step() int { return 1 }\n",
		"internal/judge/gate/rule.go":        "package gate\n\nimport \"tofu/internal/agent/loop\"\n\nfunc Decide() int { return loop.Step() }\n",
		"internal/models/transport/retry.go": "package transport\n\nfunc Retries() int { return 3 }\n",
		"internal/judge/ledger/row.go":       "package ledger\n\nimport \"tofu/internal/models/transport\"\n\nfunc N() int { return transport.Retries() }\n",
		"internal/judge/points/points.go":    "package points\n\nimport \"tofu/internal/judge/ledger\"\n\nfunc N() int { return ledger.N() }\n",
	}))

	report, err := readImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := importViolation{From: "tofu/internal/judge/gate", To: "tofu/internal/agent/loop", Rule: "under tofu/internal, " +
		"a package in tofu/internal/judge imports only " +
		"tofu/internal/judge, tofu/internal/konst, tofu/internal/platform/safe, " +
		"tofu/internal/platform/fs, tofu/internal/platform/project, tofu/internal/platform/clipboard, " +
		"tofu/internal/platform/console, tofu/internal/platform/scratch, tofu/internal/platform/credential, " +
		"tofu/internal/platform/profile, tofu/internal/platform/build, tofu/internal/platform/outside, " +
		"tofu/internal/models/transport, tofu/internal/models/classifiers/core, tofu/internal/models/classifiers/providers"}
	if len(report.Violations) != 1 || report.Violations[0] != want {
		t.Fatalf("violations %+v, want only %+v", report.Violations, want)
	}
}

func TestImportsPassesOnATreeThatObeysTheRules(t *testing.T) {
	dir := writeFixtureModule(t, everyRuleMatched(map[string]string{
		"bench/suites/judge/cost/cost.go": "package cost\n\nimport \"tofu/internal/judge/questions\"\n\nfunc Run() string { return questions.Name() }\n",
		"apps/cli/main.go":                "package main\n\nimport \"tofu/internal/judge/questions\"\n\nfunc main() { _ = questions.Name() }\n",
	}))

	out := &bytes.Buffer{}
	code := Imports(dir, false, out, out)
	if code != output.ExitOK {
		t.Fatalf("exit = %d, want %d, output %q", code, output.ExitOK, out.String())
	}
	if first := strings.SplitN(out.String(), "\n", 2)[0]; !strings.HasPrefix(first, "Imports · 18 packages · 19 rules ") || !strings.HasSuffix(first, " ✓ no violations") {
		t.Fatalf("title %q, want the counts and no violations", first)
	}
	if strings.Contains(out.String(), "✗") {
		t.Fatalf("a tree that obeys the rules printed a failure:\n%s", out.String())
	}
}

func TestImportsNamesEveryRuleItChecked(t *testing.T) {
	dir := writeFixtureModule(t, everyRuleMatched(nil))

	out := &bytes.Buffer{}
	if code := Imports(dir, true, out, out); code != output.ExitOK {
		t.Fatalf("exit = %d, output %q", code, out.String())
	}
	var envelope struct {
		Verb string
		OK   bool
		Data importsReport
	}
	decoder := json.NewDecoder(strings.NewReader(out.String()))
	if err := decoder.Decode(&envelope); err != nil || decoder.More() {
		t.Fatalf("stdout is not one JSON envelope: %v\n%s", err, out.String())
	}
	if envelope.Verb != "doctor --imports" || !envelope.OK || len(envelope.Data.Rules) != len(importRules()) {
		t.Fatalf("envelope %+v, want doctor --imports, ok, and %d rules", envelope, len(importRules()))
	}
	for i, rule := range importRules() {
		if envelope.Data.Rules[i] != rule.String() {
			t.Fatalf("rule %d is %q, want %q", i, envelope.Data.Rules[i], rule)
		}
	}
}

func TestImportsFailsOnARuleThatMatchesNoPackage(t *testing.T) {
	tree := everyRuleMatched(nil)
	delete(tree, "bench/suites/judge/cost/cost.go")
	dir := writeFixtureModule(t, tree)

	report, err := readImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	dead := map[string]string{}
	for _, violation := range report.Violations {
		dead[violation.Rule] = violation.From
	}
	want := map[string]string{
		"nothing under tofu/apps imports tofu/bench":     "tofu/bench",
		"nothing under tofu/internal imports tofu/bench": "tofu/bench",
	}
	if !maps.Equal(dead, want) || len(report.Violations) != len(want) {
		t.Fatalf("violations %+v, want one per rule matching no package: %v", report.Violations, want)
	}
	out := &bytes.Buffer{}
	if code := Imports(dir, false, out, out); code != output.ExitVerdict || !strings.Contains(out.String(), "tofu/bench  matches no package: nothing under tofu/internal imports") {
		t.Fatalf("exit %d, output\n%s\nwant exit %d and the dead rule named", code, out.String(), output.ExitVerdict)
	}
}

func TestImportsOnThisTreeFindsNothing(t *testing.T) {
	report, err := readImports(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations) > 0 {
		t.Fatalf("import violations on this tree: %+v", report.Violations)
	}
}

func TestImportsReportsAnUnreadableTreeOnStderr(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Imports(filepath.Join(t.TempDir(), "absent"), false, &out, &errOut)
	if code != output.ExitUsage || out.Len() > 0 || !strings.Contains(errOut.String(), "✗ tofu doctor --imports: unreadable:") {
		t.Fatalf("exit %d, stdout %q, stderr %q, want exit %d, nothing on stdout and the refusal on stderr", code, out.String(), errOut.String(), output.ExitUsage)
	}
}
