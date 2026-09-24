package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/turn"
)

const standInProxySource = `package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	if log := os.Getenv("FAKE_RTK_LOG"); log != "" {
		if f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintln(f, strings.Join(os.Args[1:], " "))
			f.Close()
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "rewrite" {
		fmt.Print("rtk " + strings.Join(os.Args[2:], " "))
		os.Exit(3)
	}
	fmt.Println("filtered: ok")
}
`

func standInProxyOnPath(t *testing.T) string {
	t.Helper()
	source := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module fakertk\n\ngo 1.24\n", "main.go": standInProxySource} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing the stand-in proxy: %v", err)
		}
	}
	name := "rtk"
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	dir := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(dir, name), ".")
	build.Dir = source
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatal(standInBuildFailure(source, err, out))
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	log := filepath.Join(t.TempDir(), "calls.txt")
	t.Setenv("FAKE_RTK_LOG", log)
	return log
}

func standInBuildFailure(source string, err error, out []byte) string {
	return fmt.Sprintf("the stand-in rtk this test wrote into %s did not build, so this machine has no working go toolchain and nothing in this repository is at fault: %v\n%s", source, err, out)
}

func TestAStandInThatDoesNotBuildIsNotReportedAsAnOrdinarySkip(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "proxy_test.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing proxy_test.go: %v", err)
	}
	const helper = "standInProxyOnPath"
	var declared *ast.FuncDecl
	for _, decl := range file.Decls {
		if function, ok := decl.(*ast.FuncDecl); ok && function.Name.Name == helper {
			declared = function
		}
	}
	if declared == nil {
		t.Fatalf("%s is gone, so nothing here guards how a toolchain that cannot compile is reported", helper)
	}
	var skips []string
	ast.Inspect(declared, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok && strings.HasPrefix(selector.Sel.Name, "Skip") {
			skips = append(skips, selector.Sel.Name)
		}
		return true
	})
	if len(skips) > 0 {
		t.Fatalf("%s calls %v, so a machine that cannot compile hello world goes dark as an ordinary skip", helper, skips)
	}
}

func TestAStandInBuildFailureNamesSourceOutsideThisRepositoryAndWhatTheCompilerSaid(t *testing.T) {
	source := t.TempDir()
	module, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("finding the module root: %v", err)
	}
	if inside, err := filepath.Rel(module, source); err == nil && !strings.HasPrefix(inside, "..") {
		t.Fatalf("the stand-in source is written to %s, inside this repository, so a failure there is a fact about the tree after all", inside)
	}
	for name, body := range map[string]string{"go.mod": "module fakertk\n\ngo 1.24\n", "main.go": "package main\n\nfunc main() { absent() }\n"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing a stand-in that cannot compile: %v", err)
		}
	}
	build := exec.Command("go", "build", "-o", filepath.Join(t.TempDir(), "rtk"), ".")
	build.Dir = source
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	out, err := build.CombinedOutput()
	if err == nil {
		t.Fatalf("a stand-in calling an undefined function built anyway\n%s", out)
	}
	report := standInBuildFailure(source, err, out)
	t.Logf("a stand-in that does not build reports:\n%s", strings.TrimSpace(report))
	if !strings.Contains(report, source) {
		t.Fatalf("the report does not name the directory the source was written into, so a reader looks in this repository instead: %q", report)
	}
	if !strings.Contains(report, strings.TrimSpace(string(out))) {
		t.Fatalf("the report drops what the compiler said, so the reader learns only that something failed: %q", report)
	}
}

func projectWithProxySheet(t *testing.T, sheet string) string {
	t.Helper()
	project := t.TempDir()
	if sheet != "" {
		dir := filepath.Join(project, ".tofu", "tools", "shell")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("building the project layer: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "proxy.yaml"), []byte(sheet), 0o600); err != nil {
			t.Fatalf("writing the project proxy setting: %v", err)
		}
	}
	t.Chdir(project)
	return project
}

func runOneBashCall(t *testing.T, project string) turn.ToolCallRow {
	t.Helper()
	opts, err := parseRunArgs([]string{"--dir", project, "run one command"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	built, _, err := buildRunTools(project, opts.toolSet)
	if err != nil {
		t.Skipf("this machine cannot build the run tools, so no command can be run at all: %v", err)
	}
	model := &queuedModel{decisions: []llm.Decision{
		{Build: "stub-model", Outcome: llm.OutcomeToolCalls, ToolCalls: []llm.ToolCall{
			{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"echo hi"}`)},
		}},
		{Build: "stub-model", Outcome: llm.OutcomeMessage, Content: "ran it"},
	}}
	config, _ := mustConfig(t, opts, built, runtime{model: model, spend: turn.SpendSubscription})
	row, err := turn.Run(context.Background(), config)
	if err != nil {
		t.Fatalf("turn.Run: %v", err)
	}
	if len(row.Steps) == 0 || len(row.Steps[0].ToolCalls) != 1 {
		t.Fatalf("the turn never made the one bash call: %+v", row.Steps)
	}
	return row.Steps[0].ToolCalls[0]
}

func TestAProjectTurningTheProxyOnRewritesTheCommandARunActuallyExecutes(t *testing.T) {
	log := standInProxyOnPath(t)
	project := projectWithProxySheet(t, "use: rtk\ntimeout_ms: 5000\n")

	called := runOneBashCall(t, project)
	if called.Proxy == nil {
		t.Fatalf("the run recorded no proxy with the project layer saying rtk: %+v", called)
	}
	if called.Proxy.Asked != "echo hi" || called.Proxy.Ran != "rtk echo hi" {
		t.Fatalf("the row does not carry both the asked and the run command: %+v", called.Proxy)
	}
	spawned, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the proxy was never spawned: %v", err)
	}
	t.Logf("proxy row %+v\nthe stand-in rtk was called with:\n%s", called.Proxy, spawned)
}

func TestTheShippedDefaultRecordsNoProxyAndSpawnsNothing(t *testing.T) {
	log := standInProxyOnPath(t)
	project := projectWithProxySheet(t, "")

	called := runOneBashCall(t, project)
	if called.Proxy != nil {
		t.Fatalf("the shipped default recorded a proxy: %+v", called.Proxy)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		spawned, _ := os.ReadFile(log)
		t.Fatalf("the shipped default spawned the proxy anyway: %s", spawned)
	}
}

func TestLibraryNamesTheProxySettingAndTheLayerItCameFrom(t *testing.T) {
	project := projectWithProxySheet(t, "use: rtk\ntimeout_ms: 5000\n")

	var out, errOut bytes.Buffer
	if code := libraryVerb(nil, &out, &errOut); code != exitOK {
		t.Fatalf("tofu library exited %d: %s\n%s", code, errOut.String(), out.String())
	}
	line := proxyLine(t, out.String())
	if !strings.Contains(line, "use rtk") || !strings.Contains(line, "project ") || !strings.Contains(line, filepath.Base(project)) {
		t.Fatalf("tofu library does not name the setting and the layer: %q", line)
	}
	t.Logf("tofu library\n%s", line)
}

func TestLibraryNamesARefusedProxyFileAndTheFieldThatFailed(t *testing.T) {
	projectWithProxySheet(t, "use: maybe\ntimeout_ms: 5000\n")

	var out, errOut bytes.Buffer
	if code := libraryVerb(nil, &out, &errOut); code != exitVerdict {
		t.Fatalf("a refused proxy file must fail the verb, got %d\n%s", code, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "proxy.yaml") || !strings.Contains(text, "use has to be") {
		t.Fatalf("tofu library does not name the refused file and the field:\n%s", text)
	}
	if !strings.Contains(proxyLine(t, text), "use off") {
		t.Fatalf("a refused file did not leave the setting off:\n%s", text)
	}
	t.Logf("tofu library\n%s", text)
}

func proxyLine(t *testing.T, text string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "proxy ") {
			return line
		}
	}
	t.Fatalf("tofu library says nothing about the proxy:\n%s", text)
	return ""
}
