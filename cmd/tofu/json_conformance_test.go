package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/sys"
)

type conformanceCase struct {
	name string
	args []string
	run  func(t *testing.T, args []string) (int, string, string)
}

type conformanceGroup struct {
	fixture func(t *testing.T)
	cases   []conformanceCase
}

func inProcess(_ *testing.T, args []string) (int, string, string) { return runOutput(args) }

func conformanceHome(t *testing.T) {
	isolateHome(t)
	for _, name := range sys.KeyNames() {
		t.Setenv(name, "")
	}
	project := filepath.Join(os.Getenv("USERPROFILE"), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
}

func conformanceGroups() []conformanceGroup {
	home := conformanceGroup{fixture: conformanceHome}
	for _, args := range [][]string{{"version"}, {"doctor"}, {"usage"}, {"usage", "--history"}, {"models"}, {"login", "--status"}, {"reload"}, {"browser"}, {"browser", "observe", "--tab", "7"}, {"browser", "recipes"}, {"usage", "--nope"}, {"reload", "--nope"}, {"doctor", "--nope"}, {"version", "--nope"}, {"models", "--nope"}, {"rules"}, {"session"}, {"shells"}} {
		home.cases = append(home.cases, conformanceCase{strings.Join(args, " "), args, inProcess})
	}
	rules := conformanceGroup{fixture: func(t *testing.T) { outputProject(t) }}
	for _, c := range outputCases() {
		rules.cases = append(rules.cases, conformanceCase{c.name, c.args, inProcess})
	}
	lists := conformanceGroup{fixture: func(t *testing.T) { listProject(t) }}
	for _, c := range listCases() {
		lists.cases = append(lists.cases, conformanceCase{c.name, c.args, inProcess})
	}
	jev := conformanceGroup{fixture: jevProject}
	for _, c := range jevCases() {
		jev.cases = append(jev.cases, conformanceCase{c.name, c.args, func(t *testing.T, args []string) (int, string, string) { return runJevCase(t, c, args) }})
	}
	sessions := conformanceGroup{fixture: func(t *testing.T) { sessionOutputProject(t) }}
	for _, c := range slices.DeleteFunc(sessionOutputCases(), func(c outputCase) bool { return c.name == "session-resume" || c.name == "continue" }) {
		sessions.cases = append(sessions.cases, conformanceCase{c.name, c.args, inProcess})
	}
	ledger := conformanceGroup{fixture: whyLedger}
	for _, c := range whyCases() {
		ledger.cases = append(ledger.cases, conformanceCase{c.name, c.args, func(_ *testing.T, args []string) (int, string, string) { return runWhyVerb(args) }})
	}
	notes := conformanceGroup{fixture: func(t *testing.T) { notesProject(t) }}
	for _, c := range notesCases() {
		notes.cases = append(notes.cases, conformanceCase{c.name, c.args, func(_ *testing.T, args []string) (int, string, string) { return runNote(args) }})
	}
	return []conformanceGroup{home, rules, lists, jev, sessions, ledger, notes}
}

func envelopeFault(printed string, code int) (string, string) {
	decoder := json.NewDecoder(strings.NewReader(printed))
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		return "", "stdout is not one JSON object: " + err.Error()
	}
	if decoder.More() {
		return "", "stdout holds more than one document"
	}
	for _, key := range []string{"tofu", "verb", "ok", "at", "data", "problems"} {
		if _, ok := fields[key]; !ok {
			return "", "the document has no " + key
		}
	}
	for _, char := range "<>&" {
		if escaped := fmt.Sprintf(`\u%04x`, char); strings.Contains(printed, escaped) {
			return "", "the document escapes HTML: " + escaped
		}
	}
	var verb string
	if err := json.Unmarshal(fields["verb"], &verb); err != nil || verb == "" {
		return "", "the verb is not a name"
	}
	var ok bool
	if err := json.Unmarshal(fields["ok"], &ok); err != nil || ok != (code == exitOK) {
		return "", "ok is " + string(fields["ok"]) + " and the exit code is " + strconv.Itoa(code)
	}
	return verb, ""
}

func TestEveryVerbAndUsageErrorPrintsOneEnvelopeAndNoEscapeUnderNoColour(t *testing.T) {
	var covered []string
	for _, group := range conformanceGroups() {
		textCodes := map[string]int{}
		for _, asJSON := range []bool{false, true} {
			group.fixture(t)
			t.Setenv("CLICOLOR_FORCE", "1")
			t.Setenv("NO_COLOR", "1")
			for _, c := range group.cases {
				args := c.args
				if asJSON {
					args = append(slices.Clone(args), jsonFlag)
				}
				code, out, errOut := c.run(t, args)
				if strings.ContainsRune(out+errOut, 0x1b) {
					t.Errorf("tofu %s under NO_COLOR wrote an ESC byte\nstdout %q\nstderr %q", strings.Join(args, " "), out, errOut)
				}
				if !asJSON {
					textCodes[c.name] = code
					continue
				}
				verb, fault := envelopeFault(out, code)
				switch {
				case fault != "":
					t.Errorf("tofu %s: %s\n%s", strings.Join(args, " "), fault, out)
				case errOut != "":
					t.Errorf("tofu %s wrote on stderr %q", strings.Join(args, " "), errOut)
				case code != textCodes[c.name]:
					t.Errorf("tofu %s exited %d, and the text form exited %d", strings.Join(args, " "), code, textCodes[c.name])
				}
				if fault == "" && !slices.Contains(covered, verb) {
					covered = append(covered, verb)
				}
			}
		}
	}
	slices.Sort(covered)
	for _, verb := range []string{"browser", "browser observe", "browser recipes"} {
		if !slices.Contains(covered, verb) {
			t.Errorf("tofu %s --json is not covered by a case that printed one envelope", verb)
		}
	}
	t.Logf("%d verbs, each checked for one envelope, an empty stderr, the text exit code and no ESC:\n%s", len(covered), strings.Join(covered, "\n"))
}
