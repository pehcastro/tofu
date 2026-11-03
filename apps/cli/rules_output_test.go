package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/apps/cli/output"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/testkit/cli"
	"tofu/internal/testkit/golden"
)

type outputCase struct {
	name string
	args []string
	code int
}

func outputCases() []outputCase {
	return []outputCase{
		{"list", []string{"rules", "list", "--library", "lib"}, output.ExitOK},
		{"list-usage", []string{"rules", "list", "--nope"}, output.ExitUsage},
		{"check", []string{"rules", "check", "docs", "--library", "lib"}, output.ExitVerdict},
		{"index", []string{"rules", "index", "rename a field", "internal/rule/trigger.go", "--library", "lib"}, output.ExitOK},
		{"fired", []string{"rules", "fired", "2026-09-28"}, output.ExitOK},
		{"add", []string{"rules", "add", "--global", "g1", "never write yaml by hand"}, output.ExitOK},
		{"add-refused", []string{"rules", "add", "--global", "g1", "again"}, output.ExitVerdict},
		{"add-replace", []string{"rules", "add", "--global", "--replace", "g1", "write yaml through tofu"}, output.ExitOK},
		{"off", []string{"rules", "off", "em_dash", "--reason", "quoted prose"}, output.ExitOK},
		{"overrides", []string{"rules", "overrides", "--library", "lib"}, output.ExitOK},
		{"restore", []string{"rules", "restore", "em_dash"}, output.ExitOK},
		{"restore-refused", []string{"rules", "restore", "em_dash"}, output.ExitVerdict},
		{"off-again", []string{"rules", "off", "em_dash", "--reason", "quoted prose"}, output.ExitOK},
		{"remove", []string{"rules", "remove", "em_dash"}, output.ExitOK},
		{"remove-refused", []string{"rules", "remove", "em_dash"}, output.ExitVerdict},
		{"agents-add", []string{"agents", "add", "planner", "--description", "plans the work", "--model", "claude-sub/claude-opus-5", "--tools", "read,search"}, output.ExitOK},
		{"agents-set", []string{"agents", "set", "planner", "codex-sub/gpt-5.6-sol"}, output.ExitOK},
		{"agents-remove", []string{"agents", "remove", "planner"}, output.ExitOK},
		{"agents-remove-refused", []string{"agents", "remove", "qa"}, output.ExitVerdict},
	}
}

func outputProject(t *testing.T) string {
	t.Helper()
	testkit.IsolateHome(t)
	home := os.Getenv("USERPROFILE")
	project := filepath.Join(home, "project")
	dash := string(rune(0x2014))
	files := map[string]string{
		"lib/dev/rules/comments@1.yaml":    "id: comments\ndomain: dev\nkind: structural\nchecker: comments\nconcern: code_rules\nscope: internal/**/*.go\n",
		"lib/dev/rules/release@1.yaml":     "id: release\ndomain: dev\nkind: structural\nchecker: comments\nconcern: code_rules\ncondition: (?i)\\brelease\\b\n",
		"lib/general/rules/em_dash@1.yaml": "id: em_dash\ndomain: general\nkind: structural\nconcern: output_shape\nchecker: em_dash\nmode: enforced\n",
		"lib/general/rules/brevity@1.yaml": "id: brevity\ndomain: general\nkind: human\nconcern: output_shape\ntext: answer in one line\n",
		"docs/a.md":                        "one " + dash + " two\n",
		"docs/b.md":                        "three " + dash + " four " + dash + " five\n",
	}
	for name, body := range files {
		if err := sys.WriteFile(filepath.Join(project, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(project)
	logDir, err := sys.LogDir()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	fired := `{"rule_id":"em_dash","target":"docs/a.md","mode":"enforced","blocked":true,"findings":1,"at":"` + at + `"}` + "\n" +
		`{"rule_id":"comments","target":"internal/x.go","mode":"shadow","blocked":false,"findings":2,"at":"` + at + `"}` + "\n" +
		"not json\n"
	if err := sys.WriteFile(filepath.Join(logDir, "2026-09-28"+rulesFireSuffix), []byte(fired), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func runOutput(args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRulesAndAgentsVerbsMatchTheirTextAndJSONGoldens(t *testing.T) {
	stamp := regexp.MustCompile(`"at": "[^"]*"`)
	homeValue := regexp.MustCompile(`HOME[^"]*`)
	printed := map[string]string{}
	for _, mode := range []string{"text", "json"} {
		t.Run(mode, func(t *testing.T) {
			escapedHome, _ := json.Marshal(outputProject(t))
			for _, c := range outputCases() {
				asJSON := mode == "json" && c.code != output.ExitUsage
				args := c.args
				if asJSON {
					args = append(append([]string{}, args...), output.JSONFlag)
				}
				code, out, errOut := runOutput(args)
				if code != c.code {
					t.Errorf("%s exited %d, want %d\nstdout %s\nstderr %s", c.name, code, c.code, out, errOut)
				}
				if asJSON {
					if !json.Valid([]byte(out)) || errOut != "" {
						t.Errorf("%s --json wrote stdout that is not one document, or wrote stderr %q:\n%s", c.name, errOut, out)
					}
					out = strings.ReplaceAll(out, strings.Trim(string(escapedHome), `"`), "HOME")
					out = homeValue.ReplaceAllStringFunc(out, func(path string) string { return strings.ReplaceAll(path, `\\`, "/") })
					out = strings.ReplaceAll(stamp.ReplaceAllString(out, `"at": "AT"`), konst.Version, "VERSION")
				}
				out = strings.ReplaceAll(out, time.Now().Format(time.DateOnly), "TODAY")
				printed["rules-"+c.name+"."+mode+".golden"] = "exit " + strconv.Itoa(code) + "\n--- stdout\n" + out + "--- stderr\n" + errOut
			}
		})
	}
	for name, text := range printed {
		golden.Assert(t, name, text)
	}
}

func TestNoColourWritesNoEscapeOnEitherStreamOfEveryVerb(t *testing.T) {
	for _, noColour := range []bool{false, true} {
		outputProject(t)
		t.Setenv("CLICOLOR_FORCE", "1")
		if noColour {
			t.Setenv("NO_COLOR", "1")
		}
		escaped := 0
		for _, c := range outputCases() {
			_, out, errOut := runOutput(c.args)
			if strings.ContainsRune(out+errOut, 0x1b) {
				escaped++
			}
		}
		if noColour && escaped > 0 {
			t.Errorf("with NO_COLOR, %d verbs wrote an ESC byte", escaped)
		}
		if !noColour && escaped != len(outputCases()) {
			t.Errorf("with colour forced, %d of %d verbs wrote an ESC byte, so the NO_COLOR run proves nothing", escaped, len(outputCases()))
		}
	}
}
