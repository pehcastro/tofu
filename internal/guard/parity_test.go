package guard

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
)

type parityStep struct {
	Rule  string          `json:"rule"`
	Stdin json.RawMessage `json:"stdin"`
	Raw   string          `json:"raw"`
	Msg   string          `json:"msg"`
}

type outcome struct {
	Code   int    `json:"code"`
	Deny   string `json:"deny"`
	Stderr string `json:"stderr"`
}

type parityCase struct {
	Name      string            `json:"name"`
	Fixture   string            `json:"fixture"`
	Steps     []parityStep      `json:"steps"`
	Seed      map[string]string `json:"seed"`
	SeedCount int               `json:"seedCount"`
	Want      outcome           `json:"want"`
	Files     map[string]int    `json:"files"`
	ScriptBug string            `json:"scriptBug"`
}

type side struct {
	name string
	run  func(t *testing.T, step parityStep, stdin []byte, fx fixture, base, msgFile string) outcome
}

var scriptNames = map[string]string{
	"emdash": "block-emdash", "comments": "block-comments", "library-wording": "library-is-tofu-only",
	"shell": "guard-bash", "loc-track": "loc-track", "snapshot": "snapshot", "minify-gate": "loc-gate",
	"require-ticket": "require-ticket", "own-paths": "own-paths", "ticket-log": "ticket-log", "research-used": "research-used",
}

func TestParity(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "parity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []parityCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	sides := []side{guardSide()}
	if scriptsDir(t) != "" {
		sides = append([]side{scriptSide()}, sides...)
	} else {
		t.Log("script side skipped: needs Windows, powershell and the .claude/hooks scripts")
	}
	fixtures := map[string]fixture{}
	rows := []string{"| rule | case | input | " + sides[0].name + " | " + sides[len(sides)-1].name + " | equal |"}
	for _, c := range cases {
		fx, built := fixtures[c.Fixture]
		if !built {
			fx = buildFixture(t, c.Fixture, scriptsRepo(t))
			fixtures[c.Fixture] = fx
		}
		var got []outcome
		for _, s := range sides {
			base := t.TempDir()
			got = append(got, runCase(t, s, c, fx, base))
			for pattern, want := range c.Files {
				if have := countFiles(base, expand(pattern, fx.root, base, 0)); have != want {
					t.Errorf("%s, %s: %s counts %d, want %d", c.Name, s.name, pattern, have, want)
				}
			}
		}
		equal := fmt.Sprint(got[0] == got[len(got)-1] && got[0] == c.Want)
		for i, s := range sides {
			switch {
			case got[i] == c.Want:
			case s.name == "script" && c.ScriptBug != "":
				equal = "script bug: " + c.ScriptBug
			default:
				t.Errorf("%s, %s: got %+v, want %+v", c.Name, s.name, got[i], c.Want)
			}
		}
		last := c.Steps[len(c.Steps)-1]
		rows = append(rows, fmt.Sprintf("| %s | %s | %s | %s | %s | %s |", last.Rule, c.Name, inputOf(last), shown(got[0]), shown(got[len(got)-1]), equal))
	}
	t.Log("\n" + strings.Join(rows, "\n"))
}

func runCase(t *testing.T, s side, c parityCase, fx fixture, base string) outcome {
	root := fx.root
	for _, dir := range []string{"temp", "cache"} {
		if err := os.MkdirAll(filepath.Join(base, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for n := 1; n <= max(1, c.SeedCount); n++ {
		for path, content := range c.Seed {
			target := filepath.Join(base, filepath.FromSlash(expand(path, root, base, n)))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte(expand(content, root, base, n)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	var last outcome
	for _, step := range c.Steps {
		stdin := []byte(expand(step.Raw, root, base, 0))
		if step.Stdin != nil {
			stdin = expandJSON(t, step.Stdin, root, base)
		}
		msgFile := filepath.Join(base, "COMMIT_EDITMSG")
		if err := os.WriteFile(msgFile, []byte(expand(step.Msg, root, base, 0)), 0o644); err != nil {
			t.Fatal(err)
		}
		last = s.run(t, step, stdin, fx, base, msgFile)
	}
	return last
}

func expand(text, root, base string, n int) string {
	for {
		start := strings.Index(text, "{path:")
		if start < 0 {
			break
		}
		end := start + strings.Index(text[start:], "}")
		text = text[:start] + filepath.Join(root, filepath.FromSlash(text[start+len("{path:"):end])) + text[end+1:]
	}
	return strings.NewReplacer(
		"{root}", root, "{base}", base, "{leaf}", filepath.Base(root), "{n}", fmt.Sprintf("%02d", n),
		"{emdash}", string(rune(0x2014)), "{mojibake}", string([]rune{0xe2, 0x20ac, 0x201d}), "{emdash-escape}", `\u`+"2014",
	).Replace(text)
}

func expandJSON(t *testing.T, raw json.RawMessage, root, base string) []byte {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	var walk func(any) any
	walk = func(node any) any {
		switch typed := node.(type) {
		case string:
			return expand(typed, root, base, 0)
		case map[string]any:
			for key, child := range typed {
				typed[key] = walk(child)
			}
		}
		return node
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(walk(value)); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func countFiles(base, pattern string) int {
	if lines, ok := strings.CutPrefix(pattern, "rows:"); ok {
		data, _ := os.ReadFile(filepath.Join(base, filepath.FromSlash(lines)))
		count := 0
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) != "" {
				count++
			}
		}
		return count
	}
	matches, _ := filepath.Glob(filepath.Join(base, filepath.FromSlash(pattern)))
	return len(matches)
}

func outcomeOf(code int, stdout, stderr string) outcome {
	got := outcome{Code: code, Stderr: firstLine(stderr)}
	var decision struct {
		HookSpecificOutput struct {
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if json.Unmarshal([]byte(stdout), &decision) == nil {
		got.Deny = firstLine(decision.HookSpecificOutput.PermissionDecisionReason)
	}
	return got
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimLeft(text, "\r\n"), "\n")
	return strings.TrimSpace(line)
}

func guardSide() side {
	binary := os.Getenv("TOFU_GUARD_BIN")
	if binary == "" {
		return side{name: "guard", run: func(t *testing.T, step parityStep, stdin []byte, fx fixture, base, msgFile string) outcome {
			t.Setenv("HOME", fx.home)
			t.Setenv("USERPROFILE", fx.home)
			for _, unset := range outsideAnyHarness() {
				name, _, _ := strings.Cut(unset, "=")
				t.Setenv(name, "")
			}
			var answer Answer
			switch step.Rule {
			case "git commit-msg":
				answer = CommitMsg(fx.root, msgFile)
			case "git release-docs":
				answer = ReleaseDocs(fx.root, msgFile)
			default:
				var err error
				env := Env{Root: fx.root, Temp: filepath.Join(base, "temp"), Cache: filepath.Join(base, "cache"), Boardy: fx.boardy}
				if answer, err = Hook(Rule(step.Rule), env, stdin); err != nil {
					t.Fatal(err)
				}
			}
			return outcomeOf(answer.Code, answer.Stdout, answer.Stderr)
		}}
	}
	return side{name: "tofu guard", run: func(t *testing.T, step parityStep, stdin []byte, fx fixture, base, msgFile string) outcome {
		args := append([]string{"guard"}, strings.Fields(step.Rule)...)
		if strings.HasPrefix(step.Rule, "git ") {
			args = append(args, msgFile)
		}
		return runProcess(t, exec.Command(binary, args...), stdin, fx, base)
	}}
}

func scriptSide() side {
	return side{name: "script", run: func(t *testing.T, step parityStep, stdin []byte, fx fixture, base, msgFile string) outcome {
		root := fx.root
		powershell := []string{"-NoProfile", "-ExecutionPolicy", "Bypass"}
		switch step.Rule {
		case "git commit-msg":
			powershell = append(powershell, "-File", filepath.Join(root, ".githooks", "commit-msg.ps1"), msgFile)
		case "git release-docs":
			body, _ := messageBody(msgFile)
			version := releaseSubject().FindStringSubmatch(body[0])[1]
			quoted := make([]string, len(body))
			for i, line := range body {
				quoted[i] = "'" + strings.ReplaceAll(line, "'", "''") + "'"
			}
			powershell = append(powershell, "-Command", fmt.Sprintf("& '%s' '%s' @(%s); exit $LASTEXITCODE", filepath.Join(root, ".githooks", "release-docs.ps1"), version, strings.Join(quoted, ",")))
		default:
			powershell = append(powershell, "-File", filepath.Join(root, ".claude", "hooks", scriptNames[step.Rule]+".ps1"))
		}
		return runProcess(t, exec.Command("powershell", powershell...), stdin, fx, base)
	}}
}

func runProcess(t *testing.T, cmd *exec.Cmd, stdin []byte, fx fixture, base string) outcome {
	var stdout, stderr bytes.Buffer
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = fx.root, bytes.NewReader(stdin), &stdout, &stderr
	temp, cache := filepath.Join(base, "temp"), filepath.Join(base, "cache")
	cmd.Env = append(append(os.Environ(), outsideAnyHarness()...), "CLAUDE_PROJECT_DIR="+fx.root, "TEMP="+temp, "TMP="+temp, "TMPDIR="+temp,
		"LOCALAPPDATA="+cache, "XDG_CACHE_HOME="+cache, "HOME="+fx.home, "USERPROFILE="+fx.home)
	err := cmd.Run()
	code := cmd.ProcessState.ExitCode()
	if code < 0 {
		t.Fatalf("%v: %v", cmd.Args, err)
	}
	return outcomeOf(code, stdout.String(), stderr.String())
}

func scriptsRepo(t *testing.T) string {
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for ; filepath.Dir(dir) != dir; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".claude", "hooks", "guard-bash.ps1")); err == nil {
			return dir
		}
	}
	return ""
}

func scriptsDir(t *testing.T) string {
	if _, err := exec.LookPath("powershell"); runtime.GOOS != "windows" || err != nil {
		return ""
	}
	return scriptsRepo(t)
}

type fixture struct {
	root, home string
	boardy     bool
}

func buildFixture(t *testing.T, name, repo string) fixture {
	archive, err := txtar.ParseFile(filepath.Join("testdata", name+".txtar"))
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	root, home := filepath.Join(parent, name), filepath.Join(parent, "home")
	for _, file := range archive.Files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(file.Name)), file.Data)
	}
	for _, from := range []string{filepath.Join(".claude", "hooks"), ".githooks"} {
		entries, _ := os.ReadDir(filepath.Join(repo, from))
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(repo, from, entry.Name()))
			if err == nil && repo != "" {
				writeFile(t, filepath.Join(root, from, entry.Name()), data)
			}
		}
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	tofu := cmp.Or(os.Getenv("TOFU_GUARD_BIN"), "tofu")
	built := fixture{root: root, home: home}
	for _, line := range strings.Split(strings.TrimSpace(string(archive.Comment)), "\n") {
		args := strings.Fields(line)
		built.boardy = built.boardy || strings.HasSuffix(line, "projectManagement boardy")
		switch args[0] {
		case "cp":
			data, err := os.ReadFile(filepath.Join(root, args[1]))
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(root, args[2]), data)
			continue
		case "stamp":
			stampHead(t, root, FlowsStamps)
			stampHead(t, root, TreewideStamps)
			continue
		case "tofu":
			args[0] = tofu
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir, cmd.Env = root, append(append(os.Environ(), outsideAnyHarness()...), "HOME="+home, "USERPROFILE="+home, "NO_COLOR=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", line, err, out)
		}
	}
	return built
}

func outsideAnyHarness() []string {
	return []string{"CLAUDECODE=", "CLAUDE_CODE_SESSION_ID=", "TOFU_SESSION="}
}

func writeFile(t *testing.T, path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func inputOf(step parityStep) string {
	text := step.Msg + step.Raw + string(step.Stdin)
	var event struct {
		ToolInput map[string]any `json:"tool_input"`
	}
	if json.Unmarshal(step.Stdin, &event) == nil && event.ToolInput != nil {
		text = fmt.Sprint(event.ToolInput["command"], event.ToolInput["file_path"], event.ToolInput["prompt"])
		text = strings.ReplaceAll(text, "<nil>", " ")
	}
	text = strings.Join(strings.Fields(text), " ")
	return text[:min(60, len(text))]
}

func shown(o outcome) string {
	text := fmt.Sprintf("exit %d", o.Code)
	for _, line := range []string{o.Deny, o.Stderr} {
		if line != "" {
			text += ": " + line[:min(50, len(line))]
		}
	}
	return text
}
