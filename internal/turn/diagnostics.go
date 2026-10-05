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
	"strconv"
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
	typecheckUnanswered = "typecheck has not answered yet"
)

type packageManifest struct {
	Scripts         map[string]string `json:"scripts"`
	PackageManager  string            `json:"packageManager"`
	Workspaces      json.RawMessage   `json:"workspaces"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
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

func (m packageManifest) declares(name string) bool {
	_, needed := m.Dependencies[name]
	_, developed := m.DevDependencies[name]
	return needed || developed
}

type typechecker struct {
	dir     string
	argv    []string
	prepare []string
}

func (t typechecker) prepared(ctx context.Context) error {
	if len(t.prepare) == 0 {
		return nil
	}
	cmd := exec.CommandContext(ctx, t.prepare[0], t.prepare[1:]...)
	cmd.Dir = t.dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed before the check: %w\n%s", strings.Join(t.prepare, " "), err, output)
	}
	return nil
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
	born    time.Time
	process *os.Process
	stop    func()
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

type checkWait int

const (
	waitForAnswer checkWait = iota
	waitInline
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
		if checker, skipped := typecheckerFor(project); skipped == "" {
			c.watchFor(checker)
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
	eachSourceFile(dir, func(name string) bool {
		if strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".tsx") {
			count++
		}
		return true
	})
	return count
}

func eachSourceFile(dir string, visit func(name string) bool) {
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := entry.Name()
		switch {
		case entry.IsDir() && path != dir && (name == "node_modules" || name == "dist" || strings.HasPrefix(name, ".")):
			return filepath.SkipDir
		case !entry.IsDir() && !visit(name):
			return filepath.SkipAll
		}
		return nil
	})
}

func (c *Typecheckers) Typecheck(ctx context.Context, resolved string) (string, error) {
	result, err := c.Check(ctx, resolved)
	return result.Content, err
}

func (c *Typecheckers) Check(ctx context.Context, resolved string) (Result, error) {
	since := time.Now()
	info, err := os.Stat(resolved)
	if err != nil {
		return Result{}, err
	}
	from := resolved
	if !info.IsDir() {
		from = filepath.Dir(resolved)
	}
	checker, skipped := typecheckerFor(from)
	if skipped != "" {
		return failure("typecheck skipped: " + skipped), nil
	}
	scope, _ := filepath.Rel(checker.dir, resolved)
	return c.check(ctx, checker, filepath.ToSlash(scope), since, waitForAnswer), nil
}

func (c *Typecheckers) Typechecked(ctx context.Context, resolved, result string) string {
	if !slices.Contains([]string{".ts", ".tsx", ".vue", ".svelte"}, filepath.Ext(resolved)) {
		return result
	}
	since := time.Now()
	checker, skipped := typecheckerFor(filepath.Dir(resolved))
	if skipped != "" {
		return result + "\n\ntypecheck skipped: " + skipped
	}
	relative, _ := filepath.Rel(checker.dir, resolved)
	return result + "\n\n" + c.check(ctx, checker, filepath.ToSlash(relative), since, waitInline).Content
}

func (c *Typecheckers) watchFor(checker typechecker) *tscWatch {
	if c == nil {
		return nil
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.watching[checker.dir] == nil {
		argv := append(slices.Clone(checker.argv), "--watch", "--preserveWatchOutput")
		lifetime, cancel := context.WithCancel(context.Background())
		exited := make(chan struct{})
		watch := &tscWatch{command: strings.Join(argv, " "), born: time.Now(), stop: func() { cancel(); <-exited }, changed: make(chan struct{})}
		go func() {
			defer close(exited)
			watch.run(lifetime, checker, argv)
		}()
		c.watching[checker.dir] = watch
	}
	return c.watching[checker.dir]
}

func (c *Typecheckers) check(ctx context.Context, checker typechecker, scope string, since time.Time, wait checkWait) Result {
	watch := c.watchFor(checker)
	if watch == nil {
		return coldTypecheck(ctx, checker, scope)
	}
	watch.mutex.Lock()
	warm, born := !watch.last.started.IsZero(), watch.born
	watch.mutex.Unlock()
	limit := konst.TypecheckFirstCheckMillis * time.Millisecond
	switch {
	case wait == waitInline && warm:
		limit = konst.TypecheckInlineMillis * time.Millisecond
	case wait == waitInline:
		limit = time.Until(born.Add(konst.TypecheckInlineFirstMillis * time.Millisecond))
	}
	waiting, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cycle, outcome := watch.next(waiting, since)
	took := time.Since(since).Milliseconds()
	switch {
	case outcome == watchAnswered || outcome == watchQuiet && wait == waitForAnswer:
		return tscReport(cycle.output, scope, fmt.Sprintf("typecheck: %s, %d ms", watch.command, took), cycle.failed)
	case outcome == watchCancelled:
		return failure("typecheck skipped: the turn was cancelled while " + watch.command + " ran")
	case outcome != watchLost && wait == waitInline:
		background := "typecheck: " + watch.command + " checks this change in the background, and the typecheck tool reports it"
		if cycle.started.IsZero() {
			return Result{Content: background}
		}
		return tscReport(cycle.output, scope, background+". its last finished check, from before this change", cycle.failed)
	case outcome == watchBusy:
		state := "rechecking a change"
		if cycle.started.IsZero() {
			state = "warming, on its first check of this project"
		}
		return Result{FailureText: typecheckUnanswered, Content: fmt.Sprintf(
			"typecheck: %s is still %s after %d ms. it keeps running: call typecheck again for its answer rather than running tsc through the shell", watch.command, state, took)}
	}
	c.mutex.Lock()
	if c.watching[checker.dir] == watch {
		delete(c.watching, checker.dir)
	}
	c.mutex.Unlock()
	watch.stop()
	return coldTypecheck(ctx, checker, scope)
}

func (w *tscWatch) run(lifetime context.Context, checker typechecker, argv []string) {
	defer w.settle(func() { w.ended = true })
	preparing, cancel := context.WithTimeout(lifetime, konst.TypecheckDeadlineMillis*time.Millisecond)
	defer cancel()
	if checker.prepared(preparing) != nil {
		return
	}
	output, input, err := os.Pipe()
	if err != nil {
		return
	}
	cmd := exec.CommandContext(lifetime, argv[0], argv[1:]...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = checker.dir, input, input
	tracked, err := shell.StartTracked(cmd)
	_ = input.Close()
	if err != nil {
		_ = output.Close()
		return
	}
	defer tracked.Release()
	w.process = cmd.Process
	go w.read(output)
	_ = cmd.Wait()
}

func (w *tscWatch) read(output *os.File) {
	defer func() { _ = output.Close() }()
	var lines []string
	for scanner := bufio.NewScanner(output); scanner.Scan(); {
		line := scanner.Text()
		switch {
		case strings.Contains(line, "Starting compilation in watch mode") || strings.Contains(line, "File change detected") || strings.Contains(line, " START \""):
			lines = nil
			w.settle(func() { w.running = tscCycle{started: time.Now()} })
		case strings.Contains(line, "Watching for file changes") || strings.Contains(line, " COMPLETED "):
			printed := strings.Join(lines, "\n")
			lines = nil
			w.settle(func() {
				w.last = w.running
				w.last.output, w.last.failed = printed, !strings.Contains(line, "Found 0 errors") && !strings.Contains(line, " 0 ERRORS ")
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

func typecheckerFor(from string) (typechecker, string) {
	tsconfig, found := findUp(from, tsconfigName)
	if !found {
		return typechecker{}, "no tsconfig.json in " + from + " or above it"
	}
	dir := filepath.Dir(tsconfig)
	if packages := baseOnly(dir); len(packages) > 0 {
		for i, project := range packages {
			relative, _ := filepath.Rel(dir, project)
			packages[i] = filepath.ToSlash(relative)
		}
		return typechecker{}, "tsconfig.json in " + dir + " only holds the settings its workspace packages extend and names no files of its own, so typecheck a package instead: " + strings.Join(packages, ", ")
	}
	var manifest packageManifest
	if at, found := findUp(dir, "package.json"); found {
		if body, err := os.ReadFile(at); err == nil {
			_ = json.Unmarshal(body, &manifest)
		}
	}
	manager := lockfileManager(dir, manifest)
	if manifest.declares("svelte-check") {
		return svelteChecker(dir, manager, manifest)
	}
	pkg, command, bin := "typescript", "tsc", "bin/tsc"
	if manifest.declares("vue-tsc") {
		pkg, command, bin = "vue-tsc", "vue-tsc", "bin/vue-tsc.js"
	}
	argv, skipped := checkerFor(dir, manager, pkg, command, bin)
	if skipped != "" {
		return typechecker{}, skipped
	}
	argv = append(argv, "--noEmit", "--pretty", "false", "-p", tsconfigName)
	if runsTypeScript(manifest) {
		argv = append(argv, "--erasableSyntaxOnly")
	}
	return typechecker{dir: dir, argv: argv}, ""
}

func svelteChecker(dir, manager string, manifest packageManifest) (typechecker, string) {
	argv, skipped := checkerFor(dir, manager, "svelte-check", "svelte-check", "bin/svelte-check")
	if skipped != "" {
		return typechecker{}, skipped
	}
	scripted := []string{"--tsconfig", "./" + tsconfigName}
	for _, command := range strings.Split(manifest.Scripts["check"], "&&") {
		words := strings.Fields(command)
		if at := slices.Index(words, "svelte-check"); at >= 0 {
			scripted = words[at+1:]
		}
	}
	checker := typechecker{dir: dir, argv: slices.Concat(argv, scripted, []string{"--output", "machine", "--threshold", "error"})}
	if manifest.declares("@sveltejs/kit") {
		kit, skipped := checkerFor(dir, manager, "@sveltejs/kit", "svelte-kit", "svelte-kit.js")
		if skipped != "" {
			return typechecker{}, skipped
		}
		checker.prepare = slices.Concat(kit, []string{"sync"})
	}
	return checker, ""
}

func coldTypecheck(ctx context.Context, checker typechecker, relative string) Result {
	command := strings.Join(checker.argv, " ")
	ctx, cancel := context.WithTimeout(ctx, konst.TypecheckDeadlineMillis*time.Millisecond)
	defer cancel()
	if err := checker.prepared(ctx); err != nil {
		return failure("typecheck skipped: " + err.Error())
	}
	cmd := exec.CommandContext(ctx, checker.argv[0], checker.argv[1:]...)
	cmd.Dir, cmd.WaitDelay = checker.dir, konst.BashWaitDelayMillis*time.Millisecond
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	started := time.Now()
	tracked, err := shell.StartTracked(cmd)
	if err != nil {
		return failure(fmt.Sprintf("typecheck skipped: %s did not start: %v", command, err))
	}
	defer tracked.Release()
	failed := cmd.Wait() != nil
	took := time.Since(started).Milliseconds()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return failure(fmt.Sprintf("typecheck skipped: %s ran past its %d ms limit", command, konst.TypecheckDeadlineMillis))
	case ctx.Err() != nil:
		return failure("typecheck skipped: the turn was cancelled while " + command + " ran")
	}
	return tscReport(output.String(), relative, fmt.Sprintf("typecheck: %s, %d ms", command, took), failed)
}

func checkerFor(dir, manager, pkg, command, bin string) ([]string, string) {
	installed, found := findUp(dir, "node_modules", filepath.FromSlash(pkg), "package.json")
	switch {
	case !found && pkg == "typescript":
		return machineChecker(manager)
	case !found:
		return nil, "package.json declares " + pkg + ", which is not installed under node_modules in " + dir + " or above it: install the project's dependencies first"
	}
	if _, err := exec.LookPath("node"); err != nil {
		return projectChecker(manager, command)
	}
	script, _ := filepath.Rel(dir, filepath.Join(filepath.Dir(installed), filepath.FromSlash(bin)))
	return []string{"node", script}, ""
}

func projectChecker(manager, command string) ([]string, string) {
	switch manager {
	case "bun":
		return []string{"bun", "x", command}, ""
	case "pnpm":
		return []string{"pnpm", "exec", command}, ""
	case "yarn":
		return []string{"yarn", command}, ""
	case "npm":
		return []string{"npm", "exec", "--", command}, ""
	}
	return nil, "packageManager names " + manager + ", and the check runs " + command + " only through bun, pnpm, yarn or npm"
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

func tscReport(output, scope, head string, failed bool) Result {
	var mine, global, unread []string
	errorsHere, elsewhere, inMine := 0, 0, false
	for _, printed := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		continued := strings.HasPrefix(printed, " ")
		path, line, isError := checkerError(printed)
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
	namedNoFile := failed && len(lines) == 0 && elsewhere == 0
	if namedNoFile {
		lines, summary = unread, head+", the checker failed and named no file"
	}
	report := Result{Content: summary}
	if len(lines) > 0 {
		shown := lines[:min(len(lines), konst.TypecheckLinesCap)]
		if cut := len(lines) - len(shown); cut > 0 {
			shown = append(shown, fmt.Sprintf("(%d more lines not shown)", cut))
		}
		report.Content += ":\n" + strings.Join(shown, "\n")
	}
	if namedNoFile || len(lines) > 0 {
		report.FailureText = summary
	}
	return report
}

func checkerError(printed string) (string, string, bool) {
	if located, _, found := strings.Cut(printed, "): error TS"); found {
		return located[:max(strings.LastIndex(located, "("), 0)], printed, true
	}
	_, svelte, found := strings.Cut(printed, " ERROR ")
	quoted, err := strconv.QuotedPrefix(svelte)
	if !found || err != nil {
		return "", printed, false
	}
	path, _ := strconv.Unquote(quoted)
	path = filepath.ToSlash(path)
	position, message, _ := strings.Cut(strings.TrimPrefix(svelte, quoted+" "), " ")
	if unquoted, err := strconv.Unquote(message); err == nil {
		message = unquoted
	}
	return path, path + ":" + position + ": " + message, true
}
