package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	stepTimeout    = 3 * time.Minute
	driveTimeout   = "90s"
	screenWidth    = "200"
	screenHeight   = "50"
	childProcs     = "4"
	fixedClock     = "2026-10-17T10:00:00"
	refusedURL     = "http://127.0.0.1:1"
	moveAttempts   = 40
	moveRetryAfter = 250 * time.Millisecond
)

var (
	escaped    = regexp.MustCompile(`\\n|\\U[0-9A-F]{8}`)
	timedFiles = []string{"-shm", "-wal", "/shells/pruned"}
)

var offlineURLs = []string{
	"TOFU_UPDATE_API", "TOFU_MODELS_REGISTRY_URL", "TOFU_JEV_LISTING_URL", "TOFU_NPM_REGISTRY_URL",
	"TOFU_JUDGE_ENDPOINT", "TOFU_OPENROUTER_API_URL", "TOFU_OPENROUTER_ENDPOINT", "TOFU_META_BASE_URL",
	"TOFU_CLAUDE_TOKEN_URL", "TOFU_CODEX_TOKEN_URL", "TOFU_CLAUDE_BASE_URL", "TOFU_CODEX_SOCKET_URL",
}

type build struct {
	label, exe, sum string
	literals        [][2]string
}

type work struct {
	root, home, project, hostTemp, data string
}

type sideOutput struct {
	transcript string
	files      []string
}

func newWork(root, data string) work {
	return work{root: root, home: filepath.Join(root, "home"), project: filepath.Join(root, "project"), hostTemp: filepath.Join(root, "t"), data: data}
}

func (w work) make() error {
	for _, dir := range []string{w.project, w.hostTemp, filepath.Join(w.home, "AppData", "Roaming"), filepath.Join(w.home, "AppData", "Local")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(w.project, "note.txt"), []byte("a mock note\n"), 0o644)
}

func (w work) env(extra []string) []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		for _, prefix := range []string{"TOFU_", "OPENROUTER", "TYPESAFE", "BRAVE", "META_MUSE", "CLAUDE", "CODEX", "ANTHROPIC", "OPENAI", "XDG_"} {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return strings.HasSuffix(name, "_API_KEY") || strings.HasSuffix(name, "_PROXY") ||
			slices.Contains([]string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP", "GOMAXPROCS", "NO_COLOR", "EDITOR", "VISUAL"}, name)
	})
	env = append(env, "HOME="+w.home, "USERPROFILE="+w.home, "APPDATA="+filepath.Join(w.home, "AppData", "Roaming"),
		"LOCALAPPDATA="+filepath.Join(w.home, "AppData", "Local"), "TEMP="+w.root, "TMP="+w.root, "TOFU_HOST_TEMP="+w.hostTemp,
		"GOMAXPROCS="+childProcs, "NO_COLOR=1", "TOFU_DRIVE_CLOCK="+fixedClock, "HTTP_PROXY="+refusedURL, "HTTPS_PROXY="+refusedURL, "NO_PROXY=")
	for _, name := range offlineURLs {
		env = append(env, name+"="+refusedURL)
	}
	for _, kv := range extra {
		env = append(env, w.expand(kv))
	}
	return env
}

func (w work) expand(token string) string {
	return strings.NewReplacer("$PROJECT", w.project, "$HOME", w.home, "$DATA", w.data, "$WORK", w.root).Replace(token)
}

func runSide(b build, c testCase, w work, keep string) (sideOutput, error) {
	if err := w.make(); err != nil {
		return sideOutput{}, err
	}
	var transcript strings.Builder
	for _, s := range c.steps {
		fmt.Fprintf(&transcript, "$ %s\n", s.line)
		out, err := w.runStep(b, s)
		if err != nil {
			return sideOutput{}, fmt.Errorf("%s: %w", s.line, err)
		}
		transcript.WriteString(out)
	}
	_ = w.command(b.exe, []string{"host", "stop", "--dir", w.project}, nil, "").Run()
	files, err := w.files()
	if err != nil {
		return sideOutput{}, err
	}
	return sideOutput{transcript: transcript.String(), files: files}, moveAway(w.root, keep)
}

func (w work) command(exe string, args, env []string, stdin string) *exec.Cmd {
	cmd := exec.Command(exe, args...)
	cmd.Dir, cmd.Env = w.project, w.env(env)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd
}

func (w work) runStep(b build, s step) (string, error) {
	args := make([]string, len(s.args))
	for i, arg := range s.args {
		args[i] = w.expand(arg)
	}
	switch s.verb {
	case verbTofu:
		return w.captured(b.exe, args, s)
	case verbGit:
		return w.captured("git", append([]string{"-c", "user.name=parity", "-c", "user.email=parity@example.invalid", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=develop"}, args...), s)
	case verbDrive:
		driven := []string{"drive", filepath.Join(w.data, "drive", args[0]+".steps"), "--plain", "--dir", w.project, "--home", w.home,
			"--width", screenWidth, "--height", screenHeight, "--timeout", driveTimeout}
		if cassette := filepath.Join(w.data, "drive", args[0]+".jsonl"); exists(cassette) {
			driven = append(driven, "--cassette", cassette)
		}
		out, err := w.captured(b.exe, append(driven, args[1:]...), s)
		return withoutCat(out), err
	case verbServe:
		return w.serve(b.exe, args, s.env)
	case verbFile:
		body, err := os.ReadFile(filepath.Join(w.data, args[0]))
		if err != nil {
			return "", err
		}
		return "", w.place(args[1], body)
	case verbWrite:
		return "", w.place(args[0], []byte(escaped.ReplaceAllStringFunc(args[1], unescape)))
	}
	return "", fmt.Errorf("unknown step %q", s.verb)
}

func unescape(found string) string {
	if found == `\n` {
		return "\n"
	}
	code, _ := strconv.ParseUint(found[2:], 16, 32)
	return string(rune(code))
}

func (w work) place(dest string, body []byte) error {
	target := filepath.Join(w.project, dest)
	if filepath.IsAbs(dest) {
		target = dest
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, body, 0o644)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (w work) captured(exe string, args []string, s step) (string, error) {
	var stdin string
	if s.stdin != "" {
		from := w.expand(s.stdin)
		if !filepath.IsAbs(from) {
			from = filepath.Join(w.data, from)
		}
		body, err := os.ReadFile(from)
		if err != nil {
			return "", err
		}
		stdin = string(body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), stepTimeout)
	defer cancel()
	cmd := w.command(exe, args, s.env, stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return "", err
	}
	stop := context.AfterFunc(ctx, func() { killTree(cmd.Process.Pid) })
	err := cmd.Wait()
	stop()
	if ctx.Err() != nil {
		return "", fmt.Errorf("still running after %s", stepTimeout)
	}
	return rendered(stdout.String(), stderr.String(), err)
}

func rendered(stdout, stderr string, waited error) (string, error) {
	code := 0
	var exit *exec.ExitError
	switch {
	case errors.As(waited, &exit):
		code = exit.ExitCode()
	case waited != nil:
		return stdout, waited
	}
	var out strings.Builder
	out.WriteString(stdout)
	if stdout != "" && !strings.HasSuffix(stdout, "\n") {
		out.WriteString("\n")
	}
	if stderr != "" {
		for _, line := range strings.Split(strings.TrimSuffix(stderr, "\n"), "\n") {
			out.WriteString("! " + line + "\n")
		}
	}
	out.WriteString("= exit " + strconv.Itoa(code) + "\n")
	return out.String(), nil
}

func (w work) files() ([]string, error) {
	var rows []string
	for _, root := range []struct{ dir, label string }{{w.home, "HOME"}, {w.project, "PROJECT"}} {
		err := filepath.WalkDir(root.dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || slices.ContainsFunc(timedFiles, func(suffix string) bool { return strings.HasSuffix(filepath.ToSlash(path), suffix) }) {
				return err
			}
			rel, _ := filepath.Rel(root.dir, path)
			rows = append(rows, root.label+"/"+filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func moveAway(root, keep string) error {
	if err := os.MkdirAll(filepath.Dir(keep), 0o755); err != nil {
		return err
	}
	var err error
	for range moveAttempts {
		if err = os.Rename(root, keep); err == nil {
			return nil
		}
		time.Sleep(moveRetryAfter)
	}
	return fmt.Errorf("the work folder stayed held: %w", err)
}
