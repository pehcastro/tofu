package turn

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"tofu/internal/konst"
)

const brokenLine = "broken.ts(1,7): error TS2322"

func tsProject(t *testing.T, runner, lockfile string, files map[string]string) string {
	if _, err := exec.LookPath(runner); err != nil {
		t.Skip(runner + " is not on PATH")
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"tsconfig.json": `{"compilerOptions":{"strict":true}}`, "package.json": `{"name":"fallback"}`, lockfile: "{}"} {
		if _, given := files[name]; !given {
			files[name] = body
		}
	}
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func warmWriter(t *testing.T, dir string) (*Typecheckers, func(name, body string) string) {
	checkers := NewTypecheckers()
	t.Cleanup(checkers.Close)
	tool, err := NewWriteTool(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool.Checking(checkers)
	return checkers, func(name, body string) string {
		raw, _ := json.Marshal(writeArgs{Path: name, Content: body})
		result, err := tool.Run(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		return result.Content
	}
}

func onlyWatch(t *testing.T, checkers *Typecheckers) *tscWatch {
	checkers.mutex.Lock()
	defer checkers.mutex.Unlock()
	if len(checkers.watching) != 1 {
		t.Fatalf("%d watchers are running, want one", len(checkers.watching))
	}
	for _, watch := range checkers.watching {
		return watch
	}
	return nil
}

func TestTypecheckedFallsBackWhenTheProjectHasNoTypeScript(t *testing.T) {
	for _, toolchain := range []struct{ runner, lockfile string }{{"bun", "bun.lock"}, {"npx", "package-lock.json"}} {
		t.Run(toolchain.runner, func(t *testing.T) {
			dir := tsProject(t, toolchain.runner, toolchain.lockfile, map[string]string{"broken.ts": "const count: number = \"many\";\n"})
			argv, _ := machineChecker(lockfileManager(dir, packageManifest{}))
			fetch := exec.Command(argv[0], append(argv[1:], "--version")...)
			fetch.Dir = dir
			if out, err := fetch.CombinedOutput(); err != nil {
				t.Fatalf("%s could not fetch typescript before the timed check: %v\n%s", strings.Join(argv, " "), err, out)
			}
			var cold *Typecheckers
			got := cold.Typechecked(context.Background(), filepath.Join(dir, "broken.ts"), "wrote broken.ts")
			if !strings.Contains(got, brokenLine) {
				t.Fatalf("the write result carries no tsc error:\n%s", got)
			}
		})
	}
}

func TestAWarmCheckerReportsAnErrorAndItsFixFromOneProcess(t *testing.T) {
	checkers, write := warmWriter(t, tsProject(t, "bun", "bun.lock", map[string]string{}))
	first := write("broken.ts", "const count: number = \"many\";\n")
	if !strings.Contains(first, brokenLine) || !strings.Contains(first, "--watch") {
		t.Fatalf("the first write was not reported by a watching tsc:\n%s", first)
	}
	watch := onlyWatch(t, checkers)

	second := write("broken.ts", "const count: number = 3;\n")
	if !strings.Contains(second, "errors in broken.ts: 0") || !strings.Contains(second, "--watch") {
		t.Fatalf("the fixing write was not reported clean by the watching tsc:\n%s", second)
	}
	t.Logf("first:\n%s\nsecond:\n%s", first, second)
	if onlyWatch(t, checkers) != watch {
		t.Fatal("the fixing write started a second tsc")
	}
}

func TestNewFilesWrittenOneAfterAnotherAreEachAnsweredByTheWatcher(t *testing.T) {
	_, write := warmWriter(t, tsProject(t, "bun", "bun.lock", map[string]string{"src/index.ts": "export const port = 3000;\n"}))
	for _, name := range []string{"src/routes/health.ts", "src/routes/users.ts", "src/routes/echo.ts", "src/routes/items.ts"} {
		got := write(name, "export const route: number = \"/"+filepath.Base(name)+"\";\n")
		if !strings.Contains(got, name+"(1,14): error TS2322") || !strings.Contains(got, "--watch") {
			t.Fatalf("%s was not answered by the watching tsc:\n%s", name, got)
		}
		_, typecheck, _ := strings.Cut(got, "\n\n")
		t.Log(typecheck)
	}
}

func TestTwoWritesInOneBatchShareOneWatcher(t *testing.T) {
	checkers, write := warmWriter(t, tsProject(t, "bun", "bun.lock", map[string]string{}))
	results := map[string]string{}
	var mutex sync.Mutex
	var batch sync.WaitGroup
	for name, body := range map[string]string{"a.ts": "export const a: number = \"a\";\n", "b.ts": "export const b: string = 2;\n"} {
		batch.Go(func() {
			got := write(name, body)
			mutex.Lock()
			results[name] = got
			mutex.Unlock()
		})
	}
	batch.Wait()
	for name, want := range map[string]string{"a.ts": "a.ts(1,14): error TS2322", "b.ts": "b.ts(1,14): error TS2322"} {
		if !strings.Contains(results[name], want) || !strings.Contains(results[name], "--watch") {
			t.Errorf("%s was not reported by the watching tsc:\n%s", name, results[name])
		}
	}
	onlyWatch(t, checkers)
}

func TestAKilledWatcherFallsBackColdAndIsReplaced(t *testing.T) {
	checkers, write := warmWriter(t, tsProject(t, "bun", "bun.lock", map[string]string{}))
	if first := write("broken.ts", "const count: number = \"many\";\n"); !strings.Contains(first, "--watch") {
		t.Fatalf("no watcher answered the first write:\n%s", first)
	}
	killed := onlyWatch(t, checkers)
	if err := killed.process.Kill(); err != nil {
		t.Fatal(err)
	}

	fallback := write("broken.ts", "const count: number = \"more\";\n")
	if !strings.Contains(fallback, brokenLine) || strings.Contains(fallback, "--watch") {
		t.Fatalf("the write after the kill was not a cold run reporting the error:\n%s", fallback)
	}

	third := write("broken.ts", "const count: number = \"most\";\n")
	if !strings.Contains(third, brokenLine) || !strings.Contains(third, "--watch") || onlyWatch(t, checkers) == killed {
		t.Fatalf("the write after the fallback did not start a fresh watcher:\n%s", third)
	}
}

func TestATypecheckPastItsDeadlineSaysWarmingAndKeepsTheWatcher(t *testing.T) {
	dir := tsProject(t, "bun", "bun.lock", map[string]string{"broken.ts": "const count: number = \"many\";\n"})
	checkers := NewTypecheckers()
	t.Cleanup(checkers.Close)
	hurried, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	warming, err := checkers.Typecheck(hurried, dir)
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(started); took > time.Second || !strings.Contains(warming, "still warming") {
		t.Fatalf("a check past its deadline took %s and read:\n%s", took, warming)
	}
	watch := onlyWatch(t, checkers)
	answered, err := checkers.Typecheck(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answered, brokenLine) || !strings.Contains(answered, "--watch") || onlyWatch(t, checkers) != watch {
		t.Fatalf("the watcher that was warming did not answer the next check:\n%s", answered)
	}
}

func TestATypecheckWaitsForAFirstCheckLongerThanTheWriteDeadline(t *testing.T) {
	dir := tsProject(t, "bun", "bun.lock", map[string]string{})
	output, input, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	checkers := NewTypecheckers()
	t.Cleanup(checkers.Close)
	slow := &tscWatch{command: "tsc --watch", stop: func() {}, changed: make(chan struct{})}
	checkers.watching[dir] = slow
	go slow.read(output)
	_, _ = input.WriteString("Starting compilation in watch mode...\n")
	go func() {
		time.Sleep(konst.TypecheckDeadlineMillis*time.Millisecond + time.Second)
		_, _ = input.WriteString("broken.ts(1,7): error TS2322: Type 'string' is not assignable to type 'number'.\nFound 1 error. Watching for file changes.\n")
	}()
	got, err := checkers.Typecheck(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, brokenLine) || strings.Contains(got, "still warming") {
		t.Fatalf("a first check past %d ms was not waited for:\n%s", konst.TypecheckDeadlineMillis, got)
	}
}

func TestATypecheckDuringALongRecheckWaitsForItsAnswer(t *testing.T) {
	dir := tsProject(t, "bun", "bun.lock", map[string]string{})
	output, input, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	checkers := NewTypecheckers()
	t.Cleanup(checkers.Close)
	rechecking := &tscWatch{command: "tsc --watch", stop: func() {}, changed: make(chan struct{}), born: time.Now()}
	checkers.watching[dir] = rechecking
	go rechecking.read(output)
	_, _ = input.WriteString("Starting compilation in watch mode...\nFound 0 errors. Watching for file changes.\n")
	time.Sleep(250 * time.Millisecond)
	_, _ = input.WriteString("File change detected. Starting incremental compilation...\n")
	time.Sleep(250 * time.Millisecond)
	go func() {
		time.Sleep(konst.TypecheckDeadlineMillis*time.Millisecond + time.Second)
		_, _ = input.WriteString("broken.ts(1,7): error TS2322: Type 'string' is not assignable to type 'number'.\nFound 1 error. Watching for file changes.\n")
	}()
	got, err := checkers.Typecheck(context.Background(), dir)
	if err != nil || !strings.Contains(got, brokenLine) || strings.Contains(got, "still rechecking") {
		t.Fatalf("a recheck past %d ms was not waited for: %v\n%s", konst.TypecheckDeadlineMillis, err, got)
	}
}

func TestAWriteWhileALargeProjectWarmsReturnsAtOnce(t *testing.T) {
	dir := tsProject(t, "bun", "bun.lock", map[string]string{})
	output, input, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	checkers := NewTypecheckers()
	t.Cleanup(checkers.Close)
	warming := &tscWatch{command: "tsc --watch", stop: func() {}, changed: make(chan struct{}), born: time.Now().Add(-time.Minute)}
	checkers.watching[dir] = warming
	go warming.read(output)
	_, _ = input.WriteString("Starting compilation in watch mode...\n")
	tool, err := NewWriteTool(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool.Checking(checkers)
	started := time.Now()
	result, err := tool.Run(context.Background(), json.RawMessage(`{"path":"broken.ts","content":"const count: number = 1;\n"}`))
	if took := time.Since(started); err != nil || took >= time.Second || !strings.Contains(result.Content, "in the background") {
		t.Fatalf("a write during a minute-old first check took %s: %v\n%s", took, err, result.Content)
	}
}

func watchedDirs(checkers *Typecheckers, root string) []string {
	checkers.mutex.Lock()
	defer checkers.mutex.Unlock()
	var dirs []string
	for dir := range checkers.watching {
		relative, _ := filepath.Rel(root, dir)
		dirs = append(dirs, filepath.ToSlash(relative))
	}
	slices.Sort(dirs)
	return dirs
}

func TestAWorkspaceRootThatOnlyBasesItsPackagesIsNeverWatched(t *testing.T) {
	for _, root := range []struct {
		tsconfig string
		want     []string
	}{
		{`{"compilerOptions":{"strict":true}}`, []string{"packages/a", "packages/b"}},
		{`{"compilerOptions":{"strict":true},"include":["*.ts"]}`, []string{".", "packages/a", "packages/b"}},
	} {
		dir := tsProject(t, "bun", "bun.lock", map[string]string{
			"tsconfig.json":            root.tsconfig,
			"package.json":             `{"name":"root","workspaces":["packages/*"]}`,
			"root.ts":                  "export const root = 1;\n",
			"packages/a/tsconfig.json": `{"extends":"../../tsconfig.json"}`,
			"packages/a/a.ts":          "export const a = 1;\n",
			"packages/b/tsconfig.json": `{"extends":"../../tsconfig.json"}`,
			"packages/b/b.ts":          "export const b = 1;\n",
		})
		checkers := NewTypecheckers()
		t.Cleanup(checkers.Close)
		checkers.Warm(dir)
		if got := watchedDirs(checkers, dir); !slices.Equal(got, root.want) {
			t.Fatalf("root tsconfig %s: watching %v, want %v", root.tsconfig, got, root.want)
		}
		if len(root.want) == 3 {
			continue
		}
		asked, err := checkers.Typecheck(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(asked, "packages/a") || !strings.Contains(asked, "packages/b") || len(watchedDirs(checkers, dir)) != 2 {
			t.Fatalf("a typecheck on the base config did not name the packages, or started a watcher:\n%s", asked)
		}
		t.Log(asked)
	}
}

func TestAWatcherOnInstalledTypeScriptIsOneProcess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the child process count is read from Win32_Process")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	dir := tsProject(t, "bun", "bun.lock", map[string]string{
		"node_modules/typescript/package.json": `{"name":"typescript","version":"5.9.0"}`,
		"node_modules/typescript/bin/tsc": "console.log('Starting compilation in watch mode...');\n" +
			"console.log('Found 0 errors. Watching for file changes.');\nsetInterval(() => {}, 1000);\n",
	})
	checkers := NewTypecheckers()
	t.Cleanup(checkers.Close)
	checkers.Warm(dir)
	watch := onlyWatch(t, checkers)
	if want := "node " + filepath.Join("node_modules", "typescript", "bin", "tsc") + " "; !strings.HasPrefix(watch.command, want) {
		t.Fatalf("the watcher runs %q, want it to start with %q", watch.command, want)
	}
	answered, err := checkers.Typecheck(context.Background(), dir)
	if err != nil || !strings.Contains(answered, "errors in .: 0") {
		t.Fatalf("the direct watcher did not answer: %v\n%s", err, answered)
	}
	children, err := exec.Command("powershell", "-NoProfile", "-Command",
		"@(Get-CimInstance Win32_Process -Filter 'ParentProcessId="+strconv.Itoa(watch.process.Pid)+"').Count").Output()
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.TrimSpace(string(children)); count != "0" {
		t.Fatalf("the watcher %s has %s child processes, want none", watch.command, count)
	}
}

func fakeTscChecker(errorLine string) string {
	found, exit := "Found 0 errors", "0"
	if errorLine != "" {
		found, exit = "Found 1 error", "1"
	}
	return "const watching = process.argv.includes('--watch');\n" +
		"if (watching) console.log('Starting compilation in watch mode...');\n" +
		"console.log(" + strconv.Quote(errorLine) + ");\n" +
		"if (!watching) process.exit(" + exit + ");\n" +
		"console.log('" + found + ". Watching for file changes.');\nsetInterval(() => {}, 1000);\n"
}

const fakeSvelteKit = "const fs = require('node:fs');\nfs.mkdirSync('.svelte-kit', { recursive: true });\n" +
	"fs.writeFileSync('.svelte-kit/tsconfig.json', JSON.stringify({ argv: process.argv.slice(2) }));\n"

const fakeSvelteCheck = "const fs = require('node:fs');\nconst path = require('node:path');\n" +
	"const synced = fs.existsSync('.svelte-kit/tsconfig.json');\n" +
	"const asked = process.argv.slice(2).join(' ');\n" +
	"console.log('1 START ' + JSON.stringify(process.cwd()));\n" +
	"if (!synced) console.log('2 ERROR \"tsconfig.json\" 2:13 \"File \\'$app/tsconfig\\' not found.\"');\n" +
	"else if (!asked.includes('--tsconfig ./tsconfig.json --fail-on-warnings')) console.log('2 ERROR \"tsconfig.json\" 1:1 ' + JSON.stringify('the check script was not followed: ' + asked));\n" +
	"else console.log('2 ERROR ' + JSON.stringify(['src', 'lib', 'Chip.svelte'].join(path.sep)) + ' 3:9 \"Type \\'string\\' is not assignable to type \\'number\\'.\"');\n" +
	"console.log('3 COMPLETED 4 FILES 1 ERRORS 0 WARNINGS 1 FILES_WITH_PROBLEMS');\n" +
	"if (!process.argv.includes('--watch')) process.exit(1);\nsetInterval(() => {}, 1000);\n"

const chipError = "src/lib/Chip.svelte:3:9: Type 'string' is not assignable to type 'number'."

func svelteKitFiles(kit string) map[string]string {
	return map[string]string{
		"package.json": `{"name":"svelted","scripts":{"check":"svelte-kit sync && svelte-check --tsconfig ./tsconfig.json --fail-on-warnings"},` +
			`"devDependencies":{"@sveltejs/kit":"3.0.0","svelte-check":"4.7.6","typescript":"6.0.3"}}`,
		"node_modules/typescript/package.json":       `{"name":"typescript"}`,
		"node_modules/typescript/bin/tsc":            fakeTscChecker(""),
		"node_modules/@sveltejs/kit/package.json":    `{"name":"@sveltejs/kit"}`,
		"node_modules/@sveltejs/kit/svelte-kit.js":   kit,
		"node_modules/svelte-check/package.json":     `{"name":"svelte-check"}`,
		"node_modules/svelte-check/bin/svelte-check": fakeSvelteCheck,
		"src/lib/Chip.svelte":                        "<script lang=\"ts\">\nlet { label } = $props();\nconst count: number = \"many\";\n</script>\n",
	}
}

func slowSvelteKit(millis int) string {
	return "setTimeout(() => {\n" + fakeSvelteKit + "}, " + strconv.Itoa(millis) + ");\n"
}

func TestWarmReturnsBeforeTheSvelteKitSyncAndTheFirstCheckWaitsForIt(t *testing.T) {
	dir := tsProject(t, "node", "package-lock.json", svelteKitFiles(slowSvelteKit(2000)))
	checkers := NewTypecheckers()
	t.Cleanup(checkers.Close)
	started := time.Now()
	checkers.Warm(dir)
	if took := time.Since(started); took > 500*time.Millisecond {
		t.Errorf("Warm blocked its caller for %s on a 2000 ms svelte-kit sync", took)
	}
	got, err := checkers.Check(context.Background(), filepath.Join(dir, "src", "lib", "Chip.svelte"))
	if err != nil || !strings.Contains(got.Content, "--watch") || !strings.Contains(got.Content, chipError) {
		t.Fatalf("the first check did not wait for the sync and answer from the watching svelte-check: %v\n%s", err, got.Content)
	}
	t.Logf("Warm and the first check took %s:\n%s", time.Since(started), got.Content)
}

func TestClosingDuringTheSvelteKitSyncStopsIt(t *testing.T) {
	dir := tsProject(t, "node", "package-lock.json", svelteKitFiles(slowSvelteKit(3000)))
	checkers := NewTypecheckers()
	started := time.Now()
	checkers.Warm(dir)
	watch := onlyWatch(t, checkers)
	checkers.Close()
	ended := func() bool {
		watch.mutex.Lock()
		defer watch.mutex.Unlock()
		return watch.ended
	}
	for !ended() {
		if time.Since(started) > 2*time.Second {
			t.Fatal("the watch did not end within 2 s of Close during a 3000 ms sync")
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(3500*time.Millisecond - time.Since(started))
	if _, err := os.Stat(filepath.Join(dir, ".svelte-kit", "tsconfig.json")); err == nil {
		t.Fatal("svelte-kit sync kept running after Close and wrote .svelte-kit")
	}
}

func TestCloseReturnsOnlyAfterTheWatcherProcessExited(t *testing.T) {
	checkers := NewTypecheckers()
	if _, err := checkers.Typecheck(context.Background(), tsProject(t, "bun", "bun.lock", map[string]string{})); err != nil {
		t.Fatal(err)
	}
	watch := onlyWatch(t, checkers)
	checkers.Close()
	watch.mutex.Lock()
	defer watch.mutex.Unlock()
	if !watch.ended {
		t.Fatalf("Close returned while %s was still running, so a bun it killed can set the console code page after tofu restores it", watch.command)
	}
}

func TestTypecheckRunsTheCheckerTheProjectDeclares(t *testing.T) {
	cannotReadVue := "src/main.ts(1,17): error TS2307: Cannot find module './App.vue' or its corresponding type declarations."
	vueError := "src/App.vue(2,7): error TS2322: Type 'string' is not assignable to type 'number'."
	vued := func(vueTsc string) map[string]string {
		return map[string]string{
			"package.json":                         `{"name":"vued","devDependencies":{"typescript":"6.0.3","vue-tsc":"3.3.12"}}`,
			"node_modules/typescript/package.json": `{"name":"typescript"}`,
			"node_modules/typescript/bin/tsc":      fakeTscChecker(cannotReadVue),
			"node_modules/vue-tsc/package.json":    `{"name":"vue-tsc"}`,
			"node_modules/vue-tsc/bin/vue-tsc.js":  fakeTscChecker(vueTsc),
		}
	}
	uninstalled := vued("")
	delete(uninstalled, "node_modules/vue-tsc/package.json")
	for _, project := range []struct {
		name     string
		files    map[string]string
		checked  string
		want     []string
		refusing bool
	}{
		{"vue file through vue-tsc", vued(vueError), "src/App.vue", []string{"vue-tsc", "errors in src/App.vue: 1", vueError}, true},
		{"ts file importing a vue file", vued(""), "src/main.ts", []string{"vue-tsc", "errors in src/main.ts: 0"}, false},
		{"vue-tsc declared and not installed", uninstalled, "src/main.ts", []string{"vue-tsc", "not installed"}, true},
		{"svelte file through the check script", svelteKitFiles(fakeSvelteKit), "src/lib/Chip.svelte", []string{"svelte-check", "errors in src/lib/Chip.svelte: 1", chipError}, true},
	} {
		t.Run(project.name, func(t *testing.T) {
			project.files["src/App.vue"] = "<script setup lang=\"ts\">\nconst count: number = \"many\";\n</script>\n"
			project.files["src/main.ts"] = "import App from './App.vue';\nexport default App;\n"
			project.files["src/lib/Chip.svelte"] = "<script lang=\"ts\">\nlet { label } = $props();\nconst count: number = \"many\";\n</script>\n"
			dir := tsProject(t, "node", "package-lock.json", project.files)
			checkers := NewTypecheckers()
			t.Cleanup(checkers.Close)
			got, err := checkers.Check(context.Background(), filepath.Join(dir, filepath.FromSlash(project.checked)))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range project.want {
				if !strings.Contains(got.Content, want) {
					t.Errorf("the typecheck of %s does not carry %q:\n%s", project.checked, want, got.Content)
				}
			}
			if refused := got.FailureText != ""; refused != project.refusing {
				t.Errorf("the typecheck of %s refused: %v, want %v:\n%s", project.checked, refused, project.refusing, got.Content)
			}
			t.Log(got.Content)
		})
	}
}

func TestAFileOutsideTheProgramFallsBackQuicklyAndKeepsTheWatcher(t *testing.T) {
	checkers, write := warmWriter(t, tsProject(t, "bun", "bun.lock", map[string]string{"tsconfig.json": `{"compilerOptions":{"strict":true},"include":["src"]}`}))
	if first := write("src/broken.ts", "const count: number = \"many\";\n"); !strings.Contains(first, "--watch") {
		t.Fatalf("no watcher answered the first write:\n%s", first)
	}
	watch := onlyWatch(t, checkers)

	started := time.Now()
	outside := write("scripts/loose.ts", "const loose: number = 1;\n")
	if took := time.Since(started); took >= konst.TypecheckDeadlineMillis*time.Millisecond || !strings.Contains(outside, "errors in scripts/loose.ts: 0") {
		t.Fatalf("a file tsc does not watch took %s and read:\n%s", took, outside)
	}
	if onlyWatch(t, checkers) != watch {
		t.Fatal("a file tsc does not watch cost the warm watcher")
	}
}
