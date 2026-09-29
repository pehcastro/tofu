package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/golden"
	"tofu/internal/konst"
	"tofu/internal/sys"
)

type listCase struct {
	name     string
	args     []string
	code     int
	coloured bool
}

func listCases() []listCase {
	return []listCase{
		{"agents", []string{"agents"}, exitVerdict, true},
		{"agents-usage", []string{"agents", "--nope"}, exitUsage, true},
		{"settings-set", []string{"settings", "set", "readBeforeEdit", "false"}, exitOK, true},
		{"settings-set-project", []string{"settings", "set", "--scope", "project", "theme", "Nord"}, exitOK, true},
		{"settings-set-refused", []string{"settings", "set", "readBeforeEdit", "maybe"}, exitUsage, true},
		{"settings-get", []string{"settings", "get", "theme"}, exitOK, false},
		{"settings-get-unknown", []string{"settings", "get", "nope"}, exitUsage, true},
		{"settings", []string{"settings"}, exitOK, true},
		{"docs", []string{"docs"}, exitOK, true},
		{"docs-topic", []string{"docs", "settings"}, exitOK, true},
		{"docs-search", []string{"docs", "change the colours"}, exitOK, true},
		{"docs-unknown", []string{"docs", "modles"}, exitVerdict, true},
		{"docs-nothing", []string{"docs", "zzz qqq"}, exitVerdict, true},
	}
}

func listProject(t *testing.T) string {
	t.Helper()
	isolateHome(t)
	home := os.Getenv("USERPROFILE")
	project := filepath.Join(home, "project")
	files := map[string]string{
		".tofu/agents/planner.md": "---\nname: planner\ndescription: plans the work\nmodel: claude-sub/claude-opus-5\ntools: read, search\n---\nplan it\n",
		".tofu/agents/broken.md":  "no front matter\n",
		".tofu/agents/idle.md":    "---\nname: idle\ndescription: runs on the orchestrator's model\nmodel: inherit\n---\nwait\n",
	}
	for name, body := range files {
		if err := sys.WriteFile(filepath.Join(project, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	global := "---\nname: reviewer\ndescription: reviews a diff\nmodel: codex-sub/gpt-5.6-sol\n---\nreview\n"
	if err := sys.WriteFile(filepath.Join(sys.StateDir(home), "agents", "reviewer.md"), []byte(global), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	return home
}

func TestAgentsSettingsAndDocsMatchTheirTextAndJSONGoldens(t *testing.T) {
	stamp := regexp.MustCompile(`"at": "[^"]*"`)
	homeValue := regexp.MustCompile(`HOME[^"]*`)
	printed := map[string]string{}
	for _, mode := range []string{"text", "json"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("NO_COLOR", "1")
			escapedHome, _ := json.Marshal(listProject(t))
			for _, c := range listCases() {
				asJSON := mode == "json" && c.code != exitUsage
				args := c.args
				if asJSON {
					args = append(append([]string{}, args...), jsonFlag)
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
				printed["list-output/"+c.name+"."+mode+".golden"] = "exit " + strconv.Itoa(code) + "\n--- stdout\n" + out + "--- stderr\n" + errOut
			}
		})
	}
	for name, text := range printed {
		golden.Assert(t, name, text)
	}
}

func TestAgentsSettingsAndDocsWriteNoEscapeUnderNoColour(t *testing.T) {
	for _, noColour := range []bool{false, true} {
		listProject(t)
		t.Setenv("CLICOLOR_FORCE", "1")
		t.Setenv("NO_COLOR", map[bool]string{true: "1"}[noColour])
		for _, c := range listCases() {
			_, out, errOut := runOutput(c.args)
			escaped := strings.ContainsRune(out+errOut, 0x1b)
			if noColour && escaped {
				t.Errorf("with NO_COLOR, %s wrote an ESC byte", c.name)
			}
			if !noColour && escaped != c.coloured {
				t.Errorf("with colour forced, %s wrote an ESC byte: %v, want %v", c.name, escaped, c.coloured)
			}
		}
	}
}
