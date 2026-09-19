package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func shippedRulesCatalogDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "catalog", "rules"))
	if err != nil {
		t.Fatalf("resolving the shipped rules dir: %v", err)
	}
	return abs
}

func writeEmDashFixture(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	content := "one line is fine\nthe other line breaks the rule" + string(rune(0x2014)) + "on purpose\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

func TestRulesListPrintsFourRules(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesListVerb([]string{"--catalog", shippedRulesCatalogDir(t)}, out, errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want 4: %q", len(lines), out.String())
	}
	for _, line := range lines {
		if !strings.Contains(line, "structural") || !strings.Contains(line, "shadow") {
			t.Fatalf("line %q does not carry a kind and a mode", line)
		}
	}
}

func TestRulesListJSON(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesListVerb([]string{"--catalog", shippedRulesCatalogDir(t), "--json"}, out, errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	var listing []ruleListing
	if err := json.Unmarshal(out.Bytes(), &listing); err != nil {
		t.Fatalf("unmarshalling json: %v, body %q", err, out.String())
	}
	if len(listing) != 4 {
		t.Fatalf("listing = %d, want 4: %+v", len(listing), listing)
	}
}

func TestRulesCheckShadowFireDoesNotChangeExitCode(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := t.TempDir()
	writeEmDashFixture(t, dir, "violation.md")

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesCheckVerb([]string{dir, "--catalog", shippedRulesCatalogDir(t)}, out, errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (shadow never blocks), stderr %q, stdout %q", code, exitOK, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "em_dash") {
		t.Fatalf("stdout does not mention the fired rule: %q", out.String())
	}
	if !strings.Contains(out.String(), "blocked=false") {
		t.Fatalf("stdout does not show the shadow fire as unblocked: %q", out.String())
	}
}

func TestRulesCheckEnforcedFireExitsOne(t *testing.T) {
	t.Chdir(t.TempDir())
	scratchCatalog := t.TempDir()
	enforcedRule := "id: em_dash\nkind: structural\nchecker: em_dash\nmode: enforced\n"
	if err := os.WriteFile(filepath.Join(scratchCatalog, "em_dash@1.yaml"), []byte(enforcedRule), 0o644); err != nil {
		t.Fatalf("writing the scratch catalog: %v", err)
	}

	violationDir := t.TempDir()
	writeEmDashFixture(t, violationDir, "violation.md")

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesCheckVerb([]string{violationDir, "--catalog", scratchCatalog}, out, errOut)
	if code != exitVerdict {
		t.Fatalf("exit code = %d, want %d, stderr %q, stdout %q", code, exitVerdict, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "blocked=true") {
		t.Fatalf("stdout does not show the enforced fire as blocked: %q", out.String())
	}
}

func TestRulesCheckDoesNotPromoteTheShippedCatalog(t *testing.T) {
	rules, err := readShippedRulesForTest(t)
	if err != nil {
		t.Fatalf("reading the shipped catalog: %v", err)
	}
	for _, r := range rules {
		if r.Mode != "shadow" {
			t.Fatalf("shipped rule %q is %q, this ticket promotes nothing", r.ID, r.Mode)
		}
	}
}

func readShippedRulesForTest(t *testing.T) ([]ruleListing, error) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesListVerb([]string{"--catalog", shippedRulesCatalogDir(t), "--json"}, out, errOut)
	if code != exitOK {
		t.Fatalf("rulesListVerb: exit %d, stderr %q", code, errOut.String())
	}
	var listing []ruleListing
	err := json.Unmarshal(out.Bytes(), &listing)
	return listing, err
}

func TestRulesCheckOverrideRateIsReadable(t *testing.T) {
	t.Chdir(t.TempDir())
	scratchCatalog := t.TempDir()
	enforcedRule := "id: em_dash\nkind: structural\nchecker: em_dash\nmode: enforced\n"
	if err := os.WriteFile(filepath.Join(scratchCatalog, "em_dash@1.yaml"), []byte(enforcedRule), 0o644); err != nil {
		t.Fatalf("writing the scratch catalog: %v", err)
	}

	violationDir := t.TempDir()
	writeEmDashFixture(t, violationDir, "one.md")
	writeEmDashFixture(t, violationDir, "two.md")

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesCheckVerb([]string{violationDir, "--catalog", scratchCatalog}, out, errOut)
	if code != exitVerdict {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitVerdict, errOut.String())
	}
	if !strings.Contains(out.String(), "override rate: 0/2 blocked fires overridden") {
		t.Fatalf("stdout does not carry a readable override rate: %q", out.String())
	}
}

func TestRulesCheckWritesTheFireWhereWhyCanFindIt(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := t.TempDir()
	writeEmDashFixture(t, dir, "violation.md")

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if code := rulesCheckVerb([]string{dir, "--catalog", shippedRulesCatalogDir(t)}, out, errOut); code != exitOK {
		t.Fatalf("rulesCheckVerb: exit %d, stderr %q", code, errOut.String())
	}

	logDir, err := rulesLogDir()
	if err != nil {
		t.Fatalf("rulesLogDir: %v", err)
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("reading the log dir: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), rulesFireSuffix) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no %s file in %s, the fire record was not written where why-style reading looks: %v", rulesFireSuffix, logDir, entries)
	}
}

func TestRulesVerbRejectsUnknownSubcommand(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesVerb([]string{"burn"}, out, errOut)
	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
}

func TestParseRulesCheckArgsRejectsUnknownFlag(t *testing.T) {
	if _, err := parseRulesCheckArgs([]string{"--bogus"}); err == nil {
		t.Fatal("parseRulesCheckArgs accepted an unknown flag")
	}
}
