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

type jevCase struct {
	name     string
	args     []string
	stdin    string
	status   int
	reply    string
	code     int
	usage    bool
	coloured bool
}

const (
	judgeStdin  = `{"state":{"command":"git push --force origin main"},"library":"` + runGatePoint + `"}`
	siftStdin   = "Short line.\n\nThis paragraph carries enough words to clear the floor that the length arm keeps, so it stays in the output.\n"
	refusedBody = `{"error":{"message":"vendor body that must never be printed"}}`
)

func jevCases() []jevCase {
	return []jevCase{
		{name: "check", args: []string{"check", forcePush}, status: 200, reply: askReply, code: exitOK, coloured: true},
		{name: "check-usage", args: []string{"check"}, code: exitUsage, usage: true, coloured: true},
		{name: "check-no-key", args: []string{"check", forcePush}, code: exitUsage, coloured: true},
		{name: "check-refused", args: []string{"check", forcePush}, status: 401, reply: refusedBody, code: exitUsage, coloured: true},
		{name: "judge", args: []string{"judge"}, stdin: judgeStdin, status: 200, reply: askReply, code: exitOK},
		{name: "judge-dry-run", args: []string{"judge", "--dry-run"}, stdin: judgeStdin, code: exitOK},
		{name: "judge-bad-json", args: []string{"judge"}, stdin: "nope", code: exitUsage, coloured: true},
		{name: "judge-usage", args: []string{"judge", "--nope"}, code: exitUsage, usage: true, coloured: true},
		{name: "judge-lint", args: []string{"judge", "--lint", "probe@1.yaml"}, code: exitVerdict},
		{name: "sift", args: []string{"sift"}, stdin: siftStdin, code: exitOK, coloured: true},
		{name: "sift-empty", args: []string{"sift"}, code: exitUsage, coloured: true},
		{name: "sift-usage", args: []string{"sift", "--arm", "nope"}, code: exitUsage, usage: true, coloured: true},
		{name: "lint", args: []string{"lint", "comments", "dirty"}, code: exitVerdict},
		{name: "lint-clean", args: []string{"lint", "comments", "clean"}, code: exitOK, coloured: true},
		{name: "lint-usage", args: []string{"lint"}, code: exitUsage, usage: true, coloured: true},
	}
}

func jevProject(t *testing.T) {
	t.Helper()
	isolateHome(t)
	project := filepath.Join(os.Getenv("USERPROFILE"), "project")
	files := map[string]string{
		"probe@1.yaml": "name: probe\ndomain: dev\nquestions_version: 1\n\nquestions:\n  vague:\n    type: noul\n    instructions: \"\"\n",
		"dirty/a.go":   "package dirty\n\n// a comment the lint refuses\nfunc A() {}\n",
		"clean/b.go":   "package clean\n\nfunc B() {}\n",
	}
	for name, body := range files {
		if err := sys.WriteFile(filepath.Join(project, filepath.FromSlash(name)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(project)
}

func runJevCase(t *testing.T, c jevCase, args []string) (int, string, string) {
	t.Helper()
	if c.status == 0 {
		t.Setenv(envVarName(), "")
	} else {
		stubJev(t, c.status, c.reply)
	}
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(c.stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCheckJudgeSiftAndLintMatchTheirTextAndJSONGoldens(t *testing.T) {
	stamp := regexp.MustCompile(`"at": "[^"]*"`)
	rowID := regexp.MustCompile(`\b[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9a-f]{32}\b`)
	elapsed := regexp.MustCompile(`\b[0-9]+ms\b|"elapsed_ms": [0-9]+`)
	printed := map[string]string{}
	for _, mode := range []string{"text", "json"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("NO_COLOR", "1")
			jevProject(t)
			for _, c := range jevCases() {
				asJSON := mode == "json" && !c.usage
				args := c.args
				if asJSON {
					args = append(append([]string{}, args...), jsonFlag)
				}
				code, out, errOut := runJevCase(t, c, args)
				if code != c.code {
					t.Errorf("%s exited %d, want %d\nstdout %s\nstderr %s", c.name, code, c.code, out, errOut)
				}
				if asJSON && (!json.Valid([]byte(out)) || errOut != "") {
					t.Errorf("%s --json wrote stdout that is not one document, or wrote stderr %q:\n%s", c.name, errOut, out)
				}
				if strings.Contains(out+errOut, "vendor body") {
					t.Errorf("%s printed the vendor body:\n%s%s", c.name, out, errOut)
				}
				said := "exit " + strconv.Itoa(code) + "\n--- stdout\n" + out + "--- stderr\n" + errOut
				said = rowID.ReplaceAllString(elapsed.ReplaceAllString(said, "ELAPSED"), "ROW")
				printed["jev-output/"+c.name+"."+mode+".golden"] = strings.ReplaceAll(stamp.ReplaceAllString(said, `"at": "AT"`), konst.Version, "VERSION")
			}
		})
	}
	for name, text := range printed {
		golden.Assert(t, name, text)
	}
}

func TestCheckJudgeSiftAndLintWriteNoEscapeUnderNoColour(t *testing.T) {
	for _, noColour := range []bool{false, true} {
		jevProject(t)
		t.Setenv("CLICOLOR_FORCE", "1")
		t.Setenv("NO_COLOR", map[bool]string{true: "1"}[noColour])
		for _, c := range jevCases() {
			_, out, errOut := runJevCase(t, c, c.args)
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
