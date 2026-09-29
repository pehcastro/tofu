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

	"tofu/internal/golden"
	"tofu/internal/konst"
	"tofu/internal/sys"
)

type notesCase struct {
	name string
	args []string
	code int
}

func notesCases() []notesCase {
	return []notesCase{
		{"changelog", []string{"changelog"}, exitOK},
		{"changelog-again", []string{"changelog"}, exitOK},
		{"changelog-all", []string{"changelog", "--all"}, exitOK},
		{"changelog-usage", []string{"changelog", "--every"}, exitUsage},
		{"library", []string{"library"}, exitOK},
		{"library-resolve", []string{"library", "resolve", "probe"}, exitOK},
		{"library-resolve-unknown", []string{"library", "resolve", "nope"}, exitVerdict},
		{"library-usage", []string{"library", "--nope"}, exitUsage},
		{"library-refused", []string{"library", "--dir", "refused"}, exitVerdict},
		{"migrate-dry-run", []string{"migrate", "--dry-run"}, exitOK},
		{"migrate", []string{"migrate"}, exitOK},
		{"migrate-nothing", []string{"migrate"}, exitOK},
		{"migrate-usage", []string{"migrate", "--nope"}, exitUsage},
	}
}

func notesChangelog() string {
	long := strings.Repeat("a bullet long enough to wrap across the width of a piped page ", 2)
	return "# Changelog\n\nprose above every version\n\n## " + konst.Version + " - 2026-09-29\n\nOne sentence for the version.\n\n" +
		"### Added\n\n- **bold** stays, `code` stays, and [a link](x.md) keeps its text\n- " + long + "\n\n" +
		"### Changed\n\n- one change\n\n## 0.0.1 - 2026-01-01\n\nthe first one\n"
}

func notesProject(t *testing.T) string {
	t.Helper()
	isolateHome(t)
	home := os.Getenv("USERPROFILE")
	project := filepath.Join(home, "project")
	files := map[string]string{
		".tofu/questions/probe@1.yaml":         "name: probe\ndomain: fetch\nquestions_version: 1\n\nstate:\n  - task\n\nquestions:\n  needed:\n    type: noul\n    instructions: >-\n      Is the unit\n      needed for the task.\n    criteria:\n      true: it is\n      false: it is not\n",
		".tofu/settings.json":                  `{"theme":"dark"}`,
		".tofu/sessions/HEAD":                  "turn-a\n",
		".tofu/sessions/turn-a/header.json":    `{"id":"turn-a","at":"2026-09-01T10:00:00Z"}`,
		".tofu/sessions/turn-a/body.jsonl":     `{"at":"2026-09-01T10:00:01Z","kind":"step","body":{"index":1,"assistant_text":"looking"}}` + "\n",
		".tofu/sessions/turn-b/header.json":    "{not json",
		".tofu/log/decisions.jsonl":            "{\"point\":\"tool_gate\"}\n",
		"refused/.tofu/tools/shell/proxy.yaml": "use: maybe\ntimeout_ms: 5000\n",
	}
	for name, body := range files {
		if err := sys.WriteFile(filepath.Join(project, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(project)
	return home
}

func runNote(args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	var code int
	if args[0] == "changelog" {
		code = changelogVerb(args[1:], notesChangelog(), &out, &errOut)
	} else {
		code = run(args, strings.NewReader(""), &out, &errOut)
	}
	return code, out.String(), errOut.String()
}

func TestChangelogLibraryAndMigrateMatchTheirTextAndJSONGoldens(t *testing.T) {
	stamp := regexp.MustCompile(`"at": "[^"]*"`)
	homeValue := regexp.MustCompile(`HOME[^"]*`)
	printed := map[string]string{}
	for _, mode := range []string{"text", "json"} {
		t.Run(mode, func(t *testing.T) {
			home := notesProject(t)
			escapedHome, _ := json.Marshal(home)
			for _, c := range notesCases() {
				asJSON := mode == "json" && c.code != exitUsage
				args := c.args
				if asJSON {
					args = append(append([]string{}, args...), jsonFlag)
				}
				code, out, errOut := runNote(args)
				key := sys.ProjectKey(filepath.Join(home, "project"))
				out, errOut = strings.ReplaceAll(strings.ReplaceAll(out, key, "KEY"), home, "HOME"), strings.ReplaceAll(errOut, home, "HOME")
				if code != c.code {
					t.Errorf("%s exited %d, want %d\nstdout %s\nstderr %s", c.name, code, c.code, out, errOut)
				}
				if asJSON && (!json.Valid([]byte(out)) || errOut != "") {
					t.Errorf("%s --json wrote stdout that is not one document, or wrote stderr %q:\n%s", c.name, errOut, out)
				}
				out = strings.ReplaceAll(out, strings.Trim(string(escapedHome), `"`), "HOME")
				out = homeValue.ReplaceAllStringFunc(out, func(path string) string { return strings.ReplaceAll(path, `\\`, "/") })
				out = strings.ReplaceAll(stamp.ReplaceAllString(out, `"at": "AT"`), konst.Version, "VERSION")
				errOut = strings.ReplaceAll(errOut, konst.Version, "VERSION")
				printed["notes-output/"+c.name+"."+mode+".golden"] = "exit " + strconv.Itoa(code) + "\n--- stdout\n" + out + "--- stderr\n" + errOut
			}
		})
	}
	for name, text := range printed {
		golden.Assert(t, name, text)
	}
}

func startUpNotes(t *testing.T, home string) string {
	t.Helper()
	legacy := filepath.Join(home, "legacy")
	for name, body := range map[string]string{"sessions/turn-a/header.json": `{"id":"turn-a"}`, "log/decisions.jsonl": "{}\n"} {
		if err := sys.WriteFile(filepath.Join(legacy, sys.LegacyStateDirName, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := sys.WriteFile(filepath.Join(home, sys.StateDirName, sys.CredentialFileName), []byte("OPENROUTER_KEY=sk-or-v1-thistestwroteit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var notes bytes.Buffer
	copyLegacyStateDir(&notes, legacy)
	moveProjectState(&notes, filepath.Join(home, "project"))
	moveHomeKeys(&notes)
	return notes.String()
}

func TestTheStartUpNotesAreOneLineEach(t *testing.T) {
	var notes string
	t.Run("in the project", func(t *testing.T) {
		home := notesProject(t)
		notes = strings.ReplaceAll(startUpNotes(t, home), sys.ProjectKey(filepath.Join(home, "project")), "KEY")
	})
	golden.Assert(t, "notes-output/start-up.golden", notes)
}

func TestNoColourWritesNoEscapeInTheNotesOrTheirVerbs(t *testing.T) {
	for _, noColour := range []bool{false, true} {
		home := notesProject(t)
		t.Setenv("CLICOLOR_FORCE", "1")
		if noColour {
			t.Setenv("NO_COLOR", "1")
		}
		escaped := 0
		for _, c := range notesCases() {
			_, out, errOut := runNote(c.args)
			if strings.ContainsRune(out+errOut, 0x1b) {
				escaped++
			}
		}
		notes := startUpNotes(t, home)
		if strings.ContainsRune(notes, 0x1b) {
			escaped++
		}
		if noColour && escaped > 0 {
			t.Errorf("with NO_COLOR, %d verbs and notes wrote an ESC byte", escaped)
		}
		if !noColour && escaped != len(notesCases())+1 {
			t.Errorf("with colour forced, %d of %d verbs and notes wrote an ESC byte, so the NO_COLOR run proves nothing", escaped, len(notesCases())+1)
		}
	}
}
