package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mark(b ...byte) string {
	return string(b)
}

func writeLintFixture(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

func TestLintCommentsFindsEveryKind(t *testing.T) {
	slash := mark('/', '/')
	blockOpen := mark('/', '*')
	blockClose := mark('*', '/')
	src := "package p\n" +
		slash + " a line comment\n" +
		blockOpen + " a block comment " + blockClose + "\n" +
		slash + " Exported does a thing.\n" +
		"func Exported() {}\n" +
		"var x = 1 " + slash + " trailing\n"
	path := writeLintFixture(t, src)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := lintVerb([]string{"comments", path}, out, errOut)
	if code != exitVerdict {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitVerdict, errOut.String())
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("violation lines = %d, want 4: %q", len(lines), out.String())
	}
}

func TestLintCommentsAllowsDirectivesAndURLStrings(t *testing.T) {
	slash := mark('/', '/')
	src := slash + "go:build linux\n" +
		"\n" +
		"package p\n" +
		"\n" +
		`import _ "embed"` + "\n" +
		"\n" +
		slash + "go:generate stringer -type=Kind\n" +
		"type Kind int\n" +
		"\n" +
		slash + "go:embed data.txt\n" +
		"var data string\n" +
		"\n" +
		"var x = 1 " + slash + "nolint:errcheck\n" +
		"\n" +
		`var url = "https:` + slash + `example.com"` + "\n"
	path := writeLintFixture(t, src)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := lintVerb([]string{"comments", path}, out, errOut)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitOK, errOut.String())
	}
	if out.String() != "" {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestLintCommentsJSON(t *testing.T) {
	slash := mark('/', '/')
	src := "package p\n" + slash + " a line comment\n" + "var x = 1\n"
	path := writeLintFixture(t, src)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := lintVerb([]string{"comments", path, "--json"}, out, errOut)
	if code != exitVerdict {
		t.Fatalf("exit code = %d, want %d, stderr %q", code, exitVerdict, errOut.String())
	}
	var findings []lintFinding
	if err := json.Unmarshal(out.Bytes(), &findings); err != nil {
		t.Fatalf("unmarshalling json: %v, body %q", err, out.String())
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.File == "" || f.Line == 0 || f.Column == 0 || f.Text == "" {
		t.Fatalf("finding missing a field: %+v", f)
	}
}

func TestLintCommentsRejectsUnknownVerb(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	code := lintVerb([]string{"types"}, out, errOut)
	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
}
