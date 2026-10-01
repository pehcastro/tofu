package turn

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/shell"
)

const (
	tsconfigName        = "tsconfig.json"
	erasableSyntaxError = "error TS1294:"
	typeStrippingNote   = " node runs this project's .ts files with type stripping, which refuses enum, parameter properties and namespaces that hold values"
)

type packageManifest struct {
	Scripts        map[string]string `json:"scripts"`
	PackageManager string            `json:"packageManager"`
	Workspaces     json.RawMessage   `json:"workspaces"`
}

func (m packageManifest) workspaces() []string {
	var listed []string
	if json.Unmarshal(m.Workspaces, &listed) == nil {
		return listed
	}
	var nested struct {
		Packages []string `json:"packages"`
	}
	_ = json.Unmarshal(m.Workspaces, &nested)
	return nested.Packages
}

type Typecheckers struct {
	mutex    sync.Mutex
	watching map[string]*tscWatch
}

type tscCycle struct {
	started time.Time
	output  string
	failed  bool
}

type tscWatch struct {
	command string
	process *os.Process
	stop    context.CancelFunc
	mutex   sync.Mutex
	running tscCycle
	last    tscCycle
	ended   bool
	changed chan struct{}
}

type watchOutcome int

const (
	watchAnswered watchOutcome = iota
	watchQuiet
	watchBusy
	watchLost
	watchCancelled
)

func NewTypecheckers() *Typecheckers {
	return &Typecheckers{watching: map[string]*tscWatch{}}
}

func (c *Typecheckers) Close() {
	if c == nil {
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for dir, watch := range c.watching {
		watch.stop()
		delete(c.watching, dir)
	}
}

func (c *Typecheckers) Warm(dir string) {
	projects := largestWorkspaces(dir)
	if _, err := os.Stat(filepath.Join(dir, tsconfigName)); err == nil {
		projects = append(projects, dir)
	}
	for _, project := range projects {
		if project, argv, skipped := tscCommand(project); skipped == "" {
			c.watchFor(project, argv)
		}
	}
}

func workspacePackages(dir string) []string {
	var packages []string
	var manifest packageManifest
	if body, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		_ = json.Unmarshal(body, &manifest)
	}
	for _, pattern := range manifest.workspaces() {
		found, _ := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pattern), tsconfigName))
		for _, tsconfig := range found {
			packages = append(packages, filepath.Dir(tsconfig))
		}
	}
	slices.Sort(packages)
	return slices.Compact(packages)
}

func baseOnly(dir string) []string {
	var config struct {
		Files   []string `json:"files"`
		Include []string `json:"include"`
	}
	body, err := os.ReadFile(filepath.Join(dir, tsconfigName))
	if err != nil || json.Unmarshal(body, &config) != nil || len(config.Files)+len(config.Include) > 0 {
		return nil
	}
	return workspacePackages(dir)
}

func largestWorkspaces(dir string) []string {
	packages := workspacePackages(dir)
	sizes := map[string]int{}
	for _, project := range packages {
		sizes[project] = typescriptFiles(project)
	}
	slices.SortStableFunc(packages, func(a, b string) int { return sizes[b] - sizes[a] })
	return packages[:min(len(packages), konst.TypecheckWarmPackages)]
}

func typescriptFiles(dir string) int {
	count := 0
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := entry.Name()
		switch {
		case entry.IsDir() && path != dir && (name == "node_modules" || name == "dist" || strings.HasPrefix(name, ".")):
			return filepath.SkipDir
		case strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".tsx"):
			count++
		}
		return nil
	})
	return count
}

func (c *Typecheckers) Typecheck(ctx context.Context, resolved string) (string, error) {
	since := time.Now()
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	from := resolved
	if !info.IsDir() {
		from = filepath.Dir(resolved)
	}
	dir, argv, skipped := tscCommand(from)
	if skipped != "" {
		return "typecheck skipped: " + skipped, nil
	}
	scope, _ := filepath.Rel(dir, resolved)
	report, _ := c.check(ctx, dir, argv, filepath.ToSlash(scope), since, konst.TypecheckFirstCheckMillis*time.Millisecond)
	return report, nil
}

func (c *Typecheckers) Typechecked(ctx context.Context, resolved, result string) string {
	if ext := filepath.Ext(resolved); ext != ".ts" && ext != ".tsx" {
		return result
	}
	since := time.Now()
	dir, argv, skipped := tscCommand(filepath.Dir(resolved))
	if skipped != "" {
		return result + "\n\ntypecheck skipped: " + skipped
	}
	relative, _ := filepath.Rel(dir, resolved)
	relative = filepath.ToSlash(relative)
	report, outcome := c.check(ctx, dir, argv, relative, since, konst.TypecheckDeadlineMillis*time.Millisecond)
	if outcome == watchQuiet {
		report = coldTypecheck(ctx, dir, argv, relative)
	}
	return result + "\n\n" + report
}

func (c *Typecheckers) watchFor(dir string, argv []string) *tscWatch {
	if c == nil {
		return nil
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.watching[dir] == nil {
		if watch := startWatch(dir, argv); watch != nil {
			c.watching[dir] = watch
		}
	}
	return c.watching[dir]
}

func (c *Typecheckers) check(ctx context.Context, dir string, argv []string, scope string, since time.Time, firstCheck time.Duration) (string, watchOutcome) {
	watch := c.watchFor(dir, argv)
	if watch == nil {
		return coldTypecheck(ctx, dir, argv, scope), watchLost
	}
	limit := konst.TypecheckDeadlineMillis * time.Millisecond
	watch.mutex.Lock()
	if watch.last.started.IsZero() {
		limit = firstCheck
	}
	watch.mutex.Unlock()
	waiting, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cycle, outcome := watch.next(waiting, since)
	took := time.Since(since).Milliseconds()
	switch outcome {
	case watchAnswered, watchQuiet:
		return tscReport(cycle.output, scope, fmt.Sprintf("typecheck: %s, %d ms", watch.command, took), cycle.failed), outcome
	case watchBusy:
		state := "rechecking a change"
		if cycle.started.IsZero() {
			state = "warming, on its first check of this project"
		}
		return fmt.Sprintf("typecheck: %s is still %s after %d ms. it keeps running: call typecheck again for its answer rather than running tsc through the shell", watch.command, state, took), outcome
	case watchCancelled:
		return "typecheck skipped: the turn was cancelled while " + watch.command + " ran", outcome
	}
	c.mutex.Lock()
	if c.watching[dir] == watch {
		delete(c.watching, dir)
	}
	c.mutex.Unlock()
	watch.stop()
	return coldTypecheck(ctx, dir, argv, scope), outcome
}

func startWatch(dir string, argv []string) *tscWatch {
	output, input, err := os.Pipe()
	if err != nil {
		return nil
	}
	argv = append(slices.Clone(argv), "--watch", "--preserveWatchOutput")
	lifetime, stop := context.WithCancel(context.Background())
	cmd := exec.CommandContext(lifetime, argv[0], argv[1:]...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, input, input
	tracked, err := shell.StartTracked(cmd)
	_ = input.Close()
	if err != nil {
		_ = output.Close()
		stop()
		return nil
	}
	watch := &tscWatch{command: strings.Join(argv, " "), process: cmd.Process, stop: stop, changed: make(chan struct{})}
	go watch.read(output)
	go func() {
		_ = cmd.Wait()
		tracked.Release()
		watch.settle(func() { watch.ended = true })
	}()
	return watch
}

func (w *tscWatch) read(output *os.File) {
	defer func() { _ = output.Close() }()
	var lines []string
	for scanner := bufio.NewScanner(output); scanner.Scan(); {
		line := scanner.Text()
		switch {
		case strings.Contains(line, "Starting compilation in watch mode") || strings.Contains(line, "File change detected"):
			lines = nil
			w.settle(func() { w.running = tscCycle{started: time.Now()} })
		case strings.Contains(line, "Watching for file changes"):
			printed := strings.Join(lines, "\n")
			lines = nil
			w.settle(func() {
				w.last = w.running
				w.last.output, w.last.failed = printed, !strings.Contains(line, "Found 0 errors")
			})
		default:
			lines = append(lines, line)
		}
	}
}

func (w *tscWatch) settle(update func()) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	update()
	close(w.changed)
	w.changed = make(chan struct{})
}

func (w *tscWatch) next(ctx context.Context, since time.Time) (tscCycle, watchOutcome) {
	notice := time.After(konst.TypecheckWatchNoticeMillis * time.Millisecond)
	noticed := false
	for {
		w.mutex.Lock()
		running, last, ended, changed := w.running, w.last, w.ended, w.changed
		w.mutex.Unlock()
		idle := !running.started.IsZero() && running.started.Equal(last.started)
		switch {
		case last.started.After(since):
			return last, watchAnswered
		case ended:
			return tscCycle{}, watchLost
		case noticed && idle:
			return last, watchQuiet
		}
		select {
		case <-changed:
		case <-notice:
			noticed = true
		case <-ctx.Done():
			switch {
			case !errors.Is(ctx.Err(), context.DeadlineExceeded):
				return tscCycle{}, watchCancelled
			case idle:
				return last, watchQuiet
			}
			return last, watchBusy
		}
	}
}

func tscCommand(from string) (string, []string, string) {
	tsconfig, found := findUp(from, tsconfigName)
	if !found {
		return "", nil, "no tsconfig.json in " + from + " or above it"
	}
	dir := filepath.Dir(tsconfig)
	if packages := baseOnly(dir); len(packages) > 0 {
		for i, project := range packages {
			relative, _ := filepath.Rel(dir, project)
			packages[i] = filepath.ToSlash(relative)
		}
		return "", nil, "tsconfig.json in " + dir + " only holds the settings its workspace packages extend and names no files of its own, so typecheck a package instead: " + strings.Join(packages, ", ")
	}
	var manifest packageManifest
	if at, found := findUp(dir, "package.json"); found {
		if body, err := os.ReadFile(at); err == nil {
			_ = json.Unmarshal(body, &manifest)
		}
	}
	argv, skipped := checkerFor(dir, lockfileManager(dir, manifest))
	if skipped != "" {
		return "", nil, skipped
	}
	argv = append(argv, "--noEmit", "--pretty", "false", "-p", tsconfigName)
	if runsTypeScript(manifest) {
		argv = append(argv, "--erasableSyntaxOnly")
	}
	return dir, argv, ""
}

func coldTypecheck(ctx context.Context, dir string, argv []string, relative string) string {
	command := strings.Join(argv, " ")

	ctx, cancel := context.WithTimeout(ctx, konst.TypecheckDeadlineMillis*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir, cmd.WaitDelay = dir, konst.BashWaitDelayMillis*time.Millisecond
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	started := time.Now()
	tracked, err := shell.StartTracked(cmd)
	if err != nil {
		return fmt.Sprintf("typecheck skipped: %s did not start: %v", command, err)
	}
	defer tracked.Release()
	failed := cmd.Wait() != nil
	took := time.Since(started).Milliseconds()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Sprintf("typecheck skipped: %s ran past its %d ms limit", command, konst.TypecheckDeadlineMillis)
	case ctx.Err() != nil:
		return "typecheck skipped: the turn was cancelled while " + command + " ran"
	}
	return tscReport(output.String(), relative, fmt.Sprintf("typecheck: %s, %d ms", command, took), failed)
}

func checkerFor(dir, manager string) ([]string, string) {
	installed, found := findUp(dir, "node_modules", "typescript", "package.json")
	if !found {
		return machineChecker(manager)
	}
	if _, err := exec.LookPath("node"); err != nil {
		return projectChecker(manager)
	}
	tsc, _ := filepath.Rel(dir, filepath.Join(filepath.Dir(installed), "bin", "tsc"))
	return []string{"node", tsc}, ""
}

func projectChecker(manager string) ([]string, string) {
	switch manager {
	case "bun":
		return []string{"bun", "x", "tsc"}, ""
	case "pnpm":
		return []string{"pnpm", "exec", "tsc"}, ""
	case "yarn":
		return []string{"yarn", "tsc"}, ""
	case "npm":
		return []string{"npm", "exec", "--", "tsc"}, ""
	}
	return nil, "packageManager names " + manager + ", and the check runs tsc only through bun, pnpm, yarn or npm"
}

func machineChecker(manager string) ([]string, string) {
	runners := [][]string{{"npx", "-y", "-p", "typescript", "tsc"}, {"bun", "x", "-p", "typescript", "tsc"}}
	if manager == "bun" {
		slices.Reverse(runners)
	}
	for _, argv := range runners {
		if _, err := exec.LookPath(argv[0]); err == nil {
			return argv, ""
		}
	}
	return nil, "typescript is not installed under node_modules, and neither bun nor npx is on PATH to fetch it"
}

func lockfileManager(dir string, manifest packageManifest) string {
	for _, lock := range [][2]string{{"bun.lock", "bun"}, {"bun.lockb", "bun"}, {"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"package-lock.json", "npm"}} {
		if _, found := findUp(dir, lock[0]); found {
			return lock[1]
		}
	}
	if manifest.PackageManager != "" {
		name, _, _ := strings.Cut(manifest.PackageManager, "@")
		return name
	}
	return "npm"
}

func runsTypeScript(manifest packageManifest) bool {
	for _, script := range manifest.Scripts {
		for _, command := range strings.FieldsFunc(script, func(r rune) bool { return r == '&' || r == '|' || r == ';' }) {
			words := strings.Fields(command)
			if len(words) == 0 || words[0] != "node" {
				continue
			}
			for _, word := range words[1:] {
				if ext := filepath.Ext(word); ext == ".ts" || ext == ".mts" || ext == ".cts" {
					return true
				}
			}
		}
	}
	return false
}

func tscReport(output, scope, head string, failed bool) string {
	var mine, global, unread []string
	errorsHere, elsewhere, inMine := 0, 0, false
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		continued := strings.HasPrefix(line, " ")
		located, _, isError := strings.Cut(line, "): error TS")
		path := located[:max(strings.LastIndex(located, "("), 0)]
		here := isError && (scope == "." || path == scope || strings.HasPrefix(path, scope+"/"))
		switch {
		case strings.TrimSpace(line) == "":
		case continued && inMine:
			mine = append(mine, line)
		case here:
			if strings.Contains(line, erasableSyntaxError) {
				line += typeStrippingNote
			}
			mine, errorsHere = append(mine, line), errorsHere+1
		case strings.HasPrefix(line, "error TS"):
			global = append(global, line)
		case isError:
			elsewhere++
		case !continued:
			unread = append(unread, line)
		}
		inMine = here || continued && inMine
	}
	lines := slices.Concat(global, mine)
	summary := fmt.Sprintf("%s, errors in %s: %d, in other files: %d", head, scope, errorsHere, elsewhere)
	if failed && len(lines) == 0 && elsewhere == 0 {
		lines, summary = unread, head+", tsc failed and named no file"
	}
	if len(lines) == 0 {
		return summary
	}
	shown := lines[:min(len(lines), konst.TypecheckLinesCap)]
	if cut := len(lines) - len(shown); cut > 0 {
		shown = append(shown, fmt.Sprintf("(%d more lines not shown)", cut))
	}
	return summary + ":\n" + strings.Join(shown, "\n")
}
