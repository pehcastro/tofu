package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"tofu/internal/judge/ledger"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
	shipped "tofu/library"
)

const fakeProxySource = `package main

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
	if os.Getenv("FAKE_RTK_MODE") == "panic" {
		fmt.Println("thread 'main' panicked at src/main.rs:1:1:")
		fmt.Println("failed printing to stdout: the pipe is being closed")
		fmt.Println("note: run with ` + "`" + `RUST_BACKTRACE=1` + "`" + ` environment variable to display a backtrace")
		os.Exit(101)
	}
	fmt.Println("filtered: ok")
}
`

func fakeProxyOnPath(t *testing.T) string {
	t.Helper()
	source := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing the stand-in proxy: %v", err)
		}
	}
	write("go.mod", "module fakertk\n\ngo 1.24\n")
	write("main.go", fakeProxySource)
	name := "rtk"
	if runtime.GOOS == "windows" {
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
	return fmt.Sprintf("the stand-in proxy this test wrote into %s did not build, so this machine has no working go toolchain and nothing in this repository is at fault: %v\n%s", source, err, out)
}

func TestAStandInThatDoesNotBuildIsNotReportedAsAnOrdinarySkip(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "proxy_test.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing proxy_test.go: %v", err)
	}
	const helper = "fakeProxyOnPath"
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
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0o600); err != nil {
			t.Fatalf("writing a stand-in that cannot compile: %v", err)
		}
	}
	write("go.mod", "module fakertk\n\ngo 1.24\n")
	write("main.go", "package main\n\nfunc main() { absent() }\n")
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

const (
	proxyOff = "use: off\ntimeout_ms: 5000\n"
	proxyOn  = "use: rtk\ntimeout_ms: 5000\n"
)

func proxyFrom(t *testing.T, root, sheet string) *CommandProxy {
	t.Helper()
	files := fstest.MapFS{proxySheetPath: &fstest.MapFile{Data: []byte(sheet)}}
	proxy, err := LoadCommandProxy(files, "library", root)
	if err != nil {
		t.Fatalf("loading the proxy setting: %v", err)
	}
	return proxy
}

func bashRoot(t *testing.T) (*BashTool, string) {
	t.Helper()
	root := t.TempDir()
	tool, err := NewBashTool(root)
	if err != nil {
		t.Skipf("no posix shell here, so no command can be run at all: %v", err)
	}
	return tool, root
}

type recordingPerson struct {
	asked []GateRequest
}

func (p *recordingPerson) answer(_ context.Context, request GateRequest, _ GateDecision) (PersonAnswer, error) {
	p.asked = append(p.asked, request)
	return PersonAllowedOnce, nil
}

func proxiedTurn(t *testing.T, proxy *CommandProxy, tool *BashTool) (Row, *stubGate, *recordingPerson, *session.Store) {
	t.Helper()
	store := session.NewStore(t.TempDir())
	gate := gateSaying(ledger.VerdictAsk)
	person := &recordingPerson{}
	model := &stubModel{decisions: []llm.Decision{
		toolCallDecision(llm.ToolCall{ID: "c1", Name: bashToolName, Arguments: json.RawMessage(`{"command":"echo hi"}`)}),
		messageDecision(),
	}}
	config := Config{
		Model:          model,
		Spend:          SpendAPIKey,
		Tools:          NewRegistry(tool),
		Gate:           gate,
		GateMode:       GateEnforce,
		Person:         person.answer,
		Proxy:          proxy,
		Task:           "run one command",
		Caps:           Caps{MaxSteps: 4},
		ResultBytesCap: 4096,
		ArtifactDir:    t.TempDir(),
		Sessions:       store,
	}
	row, err := Run(context.Background(), config)
	if err != nil {
		t.Fatalf("the turn returned an error: %v", err)
	}
	return row, gate, person, store
}

func recordedCall(t *testing.T, store *session.Store, id string) ToolCallRow {
	t.Helper()
	events, err := store.Body(id)
	if err != nil {
		t.Fatalf("reading the record of %s: %v", id, err)
	}
	for _, event := range events {
		if event.Kind != session.EventStep {
			continue
		}
		var step StepRow
		if err := json.Unmarshal(event.Body, &step); err != nil {
			t.Fatalf("reading a recorded step back: %v", err)
		}
		if len(step.ToolCalls) == 1 {
			return step.ToolCalls[0]
		}
	}
	t.Fatalf("no recorded step carries a tool call")
	return ToolCallRow{}
}

func TestWithTheSettingOffNothingIsSpawnedAndTheCommandIsTheOneTheModelAsked(t *testing.T) {
	log := fakeProxyOnPath(t)
	tool, root := bashRoot(t)
	proxy := proxyFrom(t, root, proxyOff)
	if proxy != nil {
		t.Fatalf("the off arm has to be a nil field, got %+v", proxy)
	}

	row, gate, _, store := proxiedTurn(t, proxy, tool)
	called := recordedCall(t, store, row.ID)
	if called.Proxy != nil {
		t.Fatalf("the row carries a proxy record with the setting off: %+v", called.Proxy)
	}
	if called.Command != "echo hi" || string(gate.requests[0].Args) != `{"command":"echo hi"}` {
		t.Fatalf("the command moved with the setting off: gate saw %s, the row ran %q", gate.requests[0].Args, called.Command)
	}
	if _, err := os.Stat(log); err == nil {
		body, _ := os.ReadFile(log)
		t.Fatalf("a process was spawned with the setting off: %s", body)
	}
	t.Logf("off: no spawn, the row ran %q and returned %d bytes", called.Command, called.ResultBytes)
}

func TestWithTheSettingOnTheGateThePersonAndTheRecordedRowCarryTheRewrittenCommand(t *testing.T) {
	fakeProxyOnPath(t)
	tool, root := bashRoot(t)
	proxy := proxyFrom(t, root, proxyOn)
	if proxy == nil {
		t.Fatal("the setting says rtk and no proxy was built")
	}

	row, gate, person, store := proxiedTurn(t, proxy, tool)
	want := `{"command":"rtk echo hi"}`
	if string(gate.requests[0].Args) != want {
		t.Fatalf("the gate decided on %s, want %s", gate.requests[0].Args, want)
	}
	if len(person.asked) != 1 || string(person.asked[0].Args) != want {
		t.Fatalf("the person was asked about %+v, want %s", person.asked, want)
	}
	called := recordedCall(t, store, row.ID)
	if string(called.Args) != want || called.Command != "rtk echo hi" {
		t.Fatalf("the recorded row reads args %s and command %q", called.Args, called.Command)
	}
	if called.Proxy == nil || called.Proxy.Asked != "echo hi" || called.Proxy.Ran != "rtk echo hi" {
		t.Fatalf("the recorded row does not carry both commands: %+v", called.Proxy)
	}
	if !strings.Contains(row.Steps[0].ToolCalls[0].Command, "rtk ") {
		t.Fatalf("the step row does not carry the run command: %+v", row.Steps[0].ToolCalls[0])
	}
	t.Logf("asked %q, ran %q, gate and person both saw %s", called.Proxy.Asked, called.Proxy.Ran, want)
}

func TestAProxyThatPanicsOnStdoutIsDetectedAndTheRawCommandRunsInstead(t *testing.T) {
	fakeProxyOnPath(t)
	t.Setenv("FAKE_RTK_MODE", "panic")
	tool, root := bashRoot(t)
	proxy := proxyFrom(t, root, proxyOn)

	row, _, _, store := proxiedTurn(t, proxy, tool)
	called := recordedCall(t, store, row.ID)
	if called.Proxy == nil || called.Proxy.Ran != "rtk echo hi" || called.Proxy.Asked != "echo hi" {
		t.Fatalf("the row does not carry both commands: %+v", called.Proxy)
	}
	if called.Command != "echo hi" || strings.Contains(called.Proxy.Note, "not on PATH") {
		t.Fatalf("the raw command did not run: %+v", called)
	}
	if called.Proxy.ProxyBytes == 0 || called.ResultBytes == 0 || called.Proxy.ProxyBytes == called.ResultBytes {
		t.Fatalf("the row does not carry both byte counts: proxy %d, command %d", called.Proxy.ProxyBytes, called.ResultBytes)
	}
	if !strings.Contains(row.Warnings[0], "panicked") {
		t.Fatalf("the fallback is not visible on the turn: %v", row.Warnings)
	}
	t.Logf("the proxy returned %d bytes of panic, the command produced %d bytes: %q", called.Proxy.ProxyBytes, called.ResultBytes, called.Proxy.Note)
}

func TestAMissingProxyBinaryRunsTheRawCommandAndDoesNotError(t *testing.T) {
	tool, root := bashRoot(t)
	t.Setenv("PATH", t.TempDir())
	proxy := proxyFrom(t, root, proxyOn)
	if proxy == nil {
		t.Fatal("a missing binary must not turn the setting off, it must run the command as asked")
	}

	row, gate, _, store := proxiedTurn(t, proxy, tool)
	called := recordedCall(t, store, row.ID)
	if called.Error != "" || called.Command != "echo hi" || string(gate.requests[0].Args) != `{"command":"echo hi"}` {
		t.Fatalf("a missing binary did not fall through to the raw command: %+v", called)
	}
	if called.Proxy == nil || !strings.Contains(called.Proxy.Note, "not on PATH") {
		t.Fatalf("the row does not say why nothing was rewritten: %+v", called.Proxy)
	}
	t.Logf("missing binary: %s, and the turn warned %q", called.Command, row.Warnings[0])
}

func TestAProjectLocalFilterFileIsRefusedAndTheRefusalIsVisible(t *testing.T) {
	fakeProxyOnPath(t)
	tool, root := bashRoot(t)
	if err := os.MkdirAll(filepath.Join(root, ".rtk"), 0o700); err != nil {
		t.Fatalf("planting the filter file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".rtk", "filters.toml"), []byte("[[filter]]\n"), 0o600); err != nil {
		t.Fatalf("planting the filter file: %v", err)
	}
	proxy := proxyFrom(t, root, proxyOn)

	row, gate, _, store := proxiedTurn(t, proxy, tool)
	called := recordedCall(t, store, row.ID)
	if called.Command != "echo hi" || string(gate.requests[0].Args) != `{"command":"echo hi"}` {
		t.Fatalf("a checkout that ships filters.toml still got a rewrite: %+v", called)
	}
	if called.Proxy == nil || !strings.Contains(called.Proxy.Note, projectFilterFile) {
		t.Fatalf("the refusal is not on the row: %+v", called.Proxy)
	}
	if len(row.Warnings) != 1 || !strings.Contains(row.Warnings[0], projectFilterFile) {
		t.Fatalf("the refusal is not visible on the turn: %v", row.Warnings)
	}
	t.Logf("refused: %s", row.Warnings[0])
}

func TestARealProxyMeasuresWhatTheModelSawWithTheSettingOnAndOff(t *testing.T) {
	if _, err := exec.LookPath("rtk"); err != nil {
		t.Skipf("this measurement needs the real rtk on PATH and this machine has none: %v", err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("finding the module root: %v", err)
	}
	tool, err := NewBashTool(root)
	if err != nil {
		t.Skipf("no posix shell here, so no command can be run at all: %v", err)
	}
	t.Setenv("RTK_DB_PATH", filepath.Join(t.TempDir(), "history.db"))
	elide, err := recall.LoadConfig()
	if err != nil {
		t.Fatalf("loading the token estimate this harness uses: %v", err)
	}

	for _, sheet := range []string{proxyOff, proxyOn} {
		model := &stubModel{decisions: []llm.Decision{
			toolCallDecision(llm.ToolCall{ID: "c1", Name: bashToolName, Arguments: json.RawMessage(`{"command":"go test -v -count=1 ./internal/recall/...","timeout_ms":600000}`)}),
			messageDecision(),
		}}
		row, err := Run(context.Background(), Config{
			Model:          model,
			Spend:          SpendAPIKey,
			Tools:          NewRegistry(tool),
			Proxy:          proxyFrom(t, root, sheet),
			Task:           "run the recall tests",
			Caps:           Caps{MaxSteps: 4},
			ResultBytesCap: konst.TurnResultBytesCap,
			ArtifactDir:    t.TempDir(),
		})
		if err != nil {
			t.Fatalf("the turn returned an error: %v", err)
		}
		seen := ""
		for _, message := range model.requests[len(model.requests)-1].Messages {
			if message.Role == llm.RoleTool {
				seen = message.Content
			}
		}
		called := row.Steps[0].ToolCalls[0]
		t.Logf("%s: ran %q, the command produced %d bytes, the model saw %d bytes and about %d tokens",
			strings.SplitN(sheet, "\n", 2)[0], called.Command, called.ResultBytes, len(seen), elide.Tokens(seen))
	}
}

func TestAVerbThatDownloadsACompilerIsRefused(t *testing.T) {
	for _, verb := range []string{"tsc", "npx"} {
		if found := fetchingVerb("rtk " + verb + " --noEmit"); found != verb {
			t.Fatalf("rtk %s was not refused, got %q", verb, found)
		}
	}
	if found := fetchingVerb("echo tsc && rtk go vet ./internal/turn"); found != "" {
		t.Fatalf("a command that only mentions a verb was refused: %q", found)
	}
}

func TestTheShippedLibraryTurnsTheProxyOn(t *testing.T) {
	proxy, err := LoadCommandProxy(shipped.Files(), "library", t.TempDir())
	if err != nil {
		t.Fatalf("loading the shipped setting: %v", err)
	}
	if proxy == nil {
		t.Fatal("the shipped library leaves the proxy off")
	}
}

func TestTheProxySheetRefusesAFieldAndAValueItDoesNotKnow(t *testing.T) {
	for _, sheet := range []string{"use: rtkx\ntimeout_ms: 5000\n", "use: rtk\ntimeout_ms: 0\n", "use: rtk\nbinary: rtk\n"} {
		files := fstest.MapFS{proxySheetPath: &fstest.MapFile{Data: []byte(sheet)}}
		proxy, err := LoadCommandProxy(files, "library", t.TempDir())
		if err == nil || proxy != nil {
			t.Fatalf("%q was accepted", sheet)
		}
		t.Logf("refused: %v", err)
	}
}

type stubGate struct {
	verdicts []ledger.Verdict
	reason   *ledger.Reason
	err      error
	requests []GateRequest
}

func gateSaying(verdicts ...ledger.Verdict) *stubGate { return &stubGate{verdicts: verdicts} }

func (g *stubGate) Decide(_ context.Context, request GateRequest) (GateDecision, error) {
	g.requests = append(g.requests, request)
	if g.err != nil {
		return GateDecision{Verdict: ledger.VerdictAsk}, g.err
	}
	verdict := g.verdicts[min(len(g.requests), len(g.verdicts))-1]
	return GateDecision{ID: "row-" + strconv.Itoa(len(g.requests)), Verdict: verdict, Reason: g.reason}, nil
}

func TestTheRewriteKeepsEveryFieldAndLeavesBackgroundPipedAndRedirectedCommandsAlone(t *testing.T) {
	fakeProxyOnPath(t)
	proxy := proxyFrom(t, t.TempDir(), proxyOn)
	for asked, want := range map[string]string{
		`{"command":"go test ./pkg/","timeout_ms":5000,"check_port":0,"later":"kept"}`: `{"check_port":0,"command":"rtk go test ./pkg/","later":"kept","timeout_ms":5000}`,
		`{"command":"go test ./pkg/ || true"}`:                                         `{"command":"rtk go test ./pkg/ || true"}`,
		`{"command":"npm run dev","background":true}`:                                  `{"command":"npm run dev","background":true}`,
		`{"command":"go test ./pkg/ | grep FAIL"}`:                                     `{"command":"go test ./pkg/ | grep FAIL"}`,
		`{"command":"go test ./pkg/ > out.txt"}`:                                       `{"command":"go test ./pkg/ > out.txt"}`,
		`{"command":"go test ./pkg/ >> out.txt"}`:                                      `{"command":"go test ./pkg/ >> out.txt"}`,
		`{"command":"cargo test 2>&1"}`:                                                `{"command":"cargo test 2>&1"}`,
	} {
		ran, row := proxy.rewrite(context.Background(), llm.ToolCall{Name: bashToolName, Arguments: json.RawMessage(asked)})
		if string(ran) != want {
			t.Errorf("%s ran as %s, want %s", asked, ran, want)
		}
		t.Logf("asked %s, ran %s, row %+v", asked, ran, *row)
	}
}
