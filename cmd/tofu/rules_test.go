package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/rule"
	"tofu/internal/sys"
)

func shippedRulesLibraryDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "library"))
	if err != nil {
		t.Fatalf("resolving the shipped rules dir: %v", err)
	}
	return abs
}

func writeEmDashFixture(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := "one line is fine\nthe other line breaks the rule" + string(rune(0x2014)) + "on purpose\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
}

const enforcedEmDashRule = "id: em_dash\ndomain: general\nkind: structural\nchecker: em_dash\nconcern: output_shape\nmode: enforced\n"

func writeProjectRulesLibrary(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "library", "general", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the project rules library: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "em_dash@1.yaml"), []byte(enforcedEmDashRule), 0o644); err != nil {
		t.Fatalf("writing the project rule: %v", err)
	}
}

func TestRulesCheckOutsideThisRepositoryReadsTheRulesInTheBinary(t *testing.T) {
	t.Chdir(t.TempDir())
	violations := t.TempDir()
	writeEmDashFixture(t, violations, "violation.md")

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesCheckVerb([]string{violations}, out, errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q, stdout %q", code, exitOK, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "rules from "+rulesFromTheBinary) {
		t.Fatalf("the verb does not name the rule set it used: %q", out.String())
	}
	if !strings.Contains(out.String(), "em_dash") {
		t.Fatalf("the shipped em dash rule did not fire: %q", out.String())
	}
}

func TestAProjectRulesLibraryOverridesTheOneInTheBinary(t *testing.T) {
	root := t.TempDir()
	writeProjectRulesLibrary(t, root)
	t.Chdir(root)
	violations := t.TempDir()
	writeEmDashFixture(t, violations, "violation.md")

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesCheckVerb([]string{violations}, out, errOut)
	if code != exitVerdict {
		t.Fatalf("exit code = %d, want %d, stderr %q, stdout %q", code, exitVerdict, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "rules from "+rulesFromTheProject) {
		t.Fatalf("the verb does not name the project rule set: %q", out.String())
	}
	if !strings.Contains(out.String(), "blocked=true") {
		t.Fatalf("the project rule did not block: %q", out.String())
	}
}

func shippedRules(t *testing.T) []rule.Rule {
	t.Helper()
	rules, err := rule.LoadDir(shippedRulesLibraryDir(t))
	if err != nil {
		t.Fatalf("loading the shipped rules off disk: %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("the shipped library holds no rule, so nothing here is tested")
	}
	return rules
}

func TestRulesListPrintsEveryShippedRule(t *testing.T) {
	shipped := shippedRules(t)
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesListVerb([]string{"--library", shippedRulesLibraryDir(t)}, out, errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != len(shipped)+1 {
		t.Fatalf("lines = %d, want an origin line and the %d rules on disk: %q", len(lines), len(shipped), out.String())
	}
	if !strings.HasPrefix(lines[0], strconv.Itoa(len(shipped))+" rules from ") {
		t.Fatalf("the first line does not count the rules and name the set: %q", lines[0])
	}
	for i, r := range shipped {
		line := lines[i+1]
		if !strings.HasPrefix(line, r.ID) {
			t.Fatalf("line %d is %q, want the rule %s", i+1, line, r.ID)
		}
		if !strings.Contains(line, string(r.Kind)) {
			t.Fatalf("line %q does not carry the kind %s the rule declares", line, r.Kind)
		}
		if carriesMode := strings.Contains(line, r.Mode.String()); carriesMode != (r.Checker != "") {
			t.Fatalf("line %q carries the mode %s: %t, and the rule names the checker %q", line, r.Mode, carriesMode, r.Checker)
		}
	}
}

func rulesListText(t *testing.T, args ...string) string {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if code := rulesListVerb(args, out, errOut); code != exitOK {
		t.Fatalf("rulesListVerb %v: exit %d, stderr %q", args, code, errOut.String())
	}
	return out.String()
}

func ruleRow(t *testing.T, listing, id string) string {
	t.Helper()
	for _, line := range strings.Split(strings.TrimRight(listing, "\n"), "\n") {
		if strings.HasPrefix(line, id+" ") {
			return line
		}
	}
	t.Fatalf("no row for %s in:\n%s", id, listing)
	return ""
}

func TestRulesListShowsNoModeWhereTheRuleDeclaresNone(t *testing.T) {
	listing := rulesListText(t, "--library", shippedRulesLibraryDir(t))
	for _, id := range []string{"quote", "flake_disagreement", "skipped_test_budget"} {
		row := ruleRow(t, listing, id)
		if strings.Contains(row, "shadow") {
			t.Errorf("%s names no checker and declares no mode, and its row still reads %q", id, row)
		}
		if strings.HasSuffix(row, " ") {
			t.Errorf("%s leaves the mode column padded with blanks: %q", id, row)
		}
	}
	if row := ruleRow(t, listing, "em_dash"); !strings.Contains(row, "shadow") {
		t.Errorf("em_dash names a checker and its row lost the mode: %q", row)
	}
}

func TestRulesListShowsModeOffWithoutAChecker(t *testing.T) {
	dir := t.TempDir()
	dropped := "id: quiet\ndomain: general\nkind: human\nconcern: identity\nmode: off\ntext: nothing\n"
	if err := os.WriteFile(filepath.Join(dir, "quiet@1.yaml"), []byte(dropped), 0o644); err != nil {
		t.Fatalf("writing the scratch library: %v", err)
	}
	if row := ruleRow(t, rulesListText(t, "--library", dir), "quiet"); !strings.HasSuffix(row, "off") {
		t.Fatalf("a rule dropped by mode off does not say so: %q", row)
	}
}

func TestRulesListNeverShowsTheShellSieveMode(t *testing.T) {
	listing := rulesListText(t, "--library", shippedRulesLibraryDir(t))
	if strings.Contains(listing, "shell_sift") || strings.Contains(listing, "enforced") {
		t.Fatalf("the shell sieve is a threshold rule this listing does not load, and its enforced mode does not switch the cut on:\n%s", listing)
	}
}

func TestRulesListJSONOmitsTheModeWhereTheRuleDeclaresNone(t *testing.T) {
	body := rulesListText(t, "--library", shippedRulesLibraryDir(t), "--json")
	var report struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		t.Fatalf("unmarshalling json: %v, body %q", err, body)
	}
	byID := map[string]map[string]any{}
	for _, one := range report.Rules {
		byID[fmt.Sprint(one["id"])] = one
	}
	for _, id := range []string{"quote", "flake_disagreement", "skipped_test_budget"} {
		if mode, carried := byID[id]["mode"]; carried {
			t.Errorf("%s declares no mode and json carries mode %v", id, mode)
		}
	}
	if byID["em_dash"]["mode"] != "shadow" {
		t.Errorf("em_dash names a checker and json lost its mode: %v", byID["em_dash"])
	}
}

func TestRulesListJSON(t *testing.T) {
	shipped := shippedRules(t)
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesListVerb([]string{"--library", shippedRulesLibraryDir(t), "--json"}, out, errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	var report ruleListReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshalling json: %v, body %q", err, out.String())
	}
	if len(report.Rules) != len(shipped) {
		t.Fatalf("listing = %d, want the %d rules on disk: %+v", len(report.Rules), len(shipped), report.Rules)
	}
	for i, r := range shipped {
		want := ruleListing{ID: r.ID, Kind: string(r.Kind)}
		if r.Checker != "" {
			want.Mode = r.Mode.String()
		}
		if report.Rules[i] != want {
			t.Fatalf("listing %d is %+v, want %+v for the rule %s on disk", i, report.Rules[i], want, r.ID)
		}
	}
	if report.Origin == "" {
		t.Fatalf("the report does not name the rule set it used: %+v", report)
	}
}

func TestRulesCheckShadowFireDoesNotChangeExitCode(t *testing.T) {
	library := shippedRulesLibraryDir(t)
	t.Chdir(t.TempDir())
	dir := t.TempDir()
	writeEmDashFixture(t, dir, "violation.md")

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesCheckVerb([]string{dir, "--library", library}, out, errOut)
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

func writeEnforcedEmDashLibrary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "em_dash@1.yaml"), []byte(enforcedEmDashRule), 0o644); err != nil {
		t.Fatalf("writing the scratch library: %v", err)
	}
	return dir
}

func TestRulesCheckEnforcedFireExitsOne(t *testing.T) {
	t.Chdir(t.TempDir())
	scratchLibrary := writeEnforcedEmDashLibrary(t)
	violationDir := t.TempDir()
	writeEmDashFixture(t, violationDir, "violation.md")

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesCheckVerb([]string{violationDir, "--library", scratchLibrary}, out, errOut)
	if code != exitVerdict {
		t.Fatalf("exit code = %d, want %d, stderr %q, stdout %q", code, exitVerdict, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "blocked=true") {
		t.Fatalf("stdout does not show the enforced fire as blocked: %q", out.String())
	}
}

func TestRulesCheckDoesNotPromoteTheShippedLibrary(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesListVerb([]string{"--library", shippedRulesLibraryDir(t), "--json"}, out, errOut)
	if code != exitOK {
		t.Fatalf("rulesListVerb: exit %d, stderr %q", code, errOut.String())
	}
	var report ruleListReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("reading the shipped library: %v", err)
	}
	for _, r := range report.Rules {
		if r.Mode != "" && r.Mode != "shadow" {
			t.Fatalf("shipped rule %q is %q, this ticket promotes nothing", r.ID, r.Mode)
		}
	}
}

func TestRulesCheckCountsTheFiresItBlocked(t *testing.T) {
	t.Chdir(t.TempDir())
	scratchLibrary := writeEnforcedEmDashLibrary(t)
	violationDir := t.TempDir()
	writeEmDashFixture(t, violationDir, "one.md")
	writeEmDashFixture(t, violationDir, "two.md")
	if err := os.WriteFile(filepath.Join(violationDir, "clean.md"), []byte("no rule fires here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := rulesCheckVerb([]string{violationDir, "--library", scratchLibrary, "--json"}, out, errOut)
	if code != exitVerdict {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitVerdict, errOut.String())
	}
	var report ruleCheckReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("reading the check report: %v", err)
	}
	if len(report.Fires) != 2 || report.Blocked != 2 {
		t.Fatalf("two violating files and one clean one gave %d fires and %d blocked: %+v", len(report.Fires), report.Blocked, report)
	}
	if strings.Contains(out.String(), "override") {
		t.Fatalf("the report still carries an override number nothing can move: %q", out.String())
	}
}

func TestRulesCheckWritesTheFireWhereWhyCanFindIt(t *testing.T) {
	library := shippedRulesLibraryDir(t)
	t.Chdir(t.TempDir())
	dir := t.TempDir()
	writeEmDashFixture(t, dir, "violation.md")

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if code := rulesCheckVerb([]string{dir, "--library", library}, out, errOut); code != exitOK {
		t.Fatalf("rulesCheckVerb: exit %d, stderr %q", code, errOut.String())
	}

	logDir, err := sys.LogDir()
	if err != nil {
		t.Fatalf("sys.LogDir: %v", err)
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

func TestRulesCheckReachesTheRulesWhoseSubjectIsAPackage(t *testing.T) {
	library := shippedRulesLibraryDir(t)
	tree, err := filepath.Abs(filepath.Join("..", "..", "internal", "rule", "testdata", "tree"))
	if err != nil {
		t.Fatalf("resolving the fixture tree: %v", err)
	}
	t.Chdir(t.TempDir())

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if code := rulesCheckVerb([]string{tree, "--library", library, "--json"}, out, errOut); code != exitOK {
		t.Fatalf("rulesCheckVerb: exit %d, stderr %q", code, errOut.String())
	}
	var report ruleCheckReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("reading the check report: %v, body %q", err, out.String())
	}
	fired := map[string]int{}
	for _, f := range report.Fires {
		fired[f.RuleID]++
	}
	for _, id := range []string{"test_assertion", "test_mock_boundary", "test_boundary_cases"} {
		if fired[id] != 2 {
			t.Errorf("%s fired on %d packages of the fixture tree, want 2: %v", id, fired[id], fired)
		}
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
