package turn

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/shell"
)

const (
	testReplyMarker  = "TOFU-TEST-REPLY "
	testRunnerScript = `const { createVitest } = await import('vitest/node');
const { createInterface } = await import('node:readline');
const workers = Number(process.env.TOFU_TEST_WORKERS);
const vitest = await createVitest('test', { watch: true, reporters: [], coverage: { enabled: false }, maxWorkers: workers, minWorkers: 1 });
vitest.scheduleRerun = async () => {};
await vitest.init();
const message = (error) => String((error && (error.stack || error.message)) || error);
const run = async (request) => {
  let specs = vitest.getModuleSpecifications(request.file);
  if (specs.length === 0) {
    vitest.config.related = [request.file];
    specs = await vitest.getRelevantTestSpecifications();
    vitest.config.related = undefined;
  }
  if (specs.length === 0) return { id: request.id, none: true };
  const result = await vitest.runTestSpecifications(specs);
  return {
    id: request.id,
    errors: result.unhandledErrors.map(message),
    modules: result.testModules.map((module) => ({
      file: module.moduleId,
      errors: module.errors().map((error) => error.message),
      tests: [...module.children.allTests()].map((test) => ({
        name: test.fullName,
        state: test.result().state,
        errors: (test.result().errors || []).map((error) => error.message),
      })),
    })),
  };
};
for await (const line of createInterface({ input: process.stdin })) {
  const request = JSON.parse(line);
  let reply;
  try {
    reply = await run(request);
  } catch (error) {
    reply = { id: request.id, failure: message(error) };
  }
  process.stdout.write('\n` + testReplyMarker + `' + JSON.stringify(reply) + '\n');
}
await vitest.close();
process.exit(0);
`
)

type TestRunners struct {
	mutex   sync.Mutex
	running map[string]*testRunner
}

type testRunner struct {
	process *os.Process
	stop    func()
	input   io.Writer
	asking  sync.Mutex
	asked   int
	mutex   sync.Mutex
	answers map[int]testReply
	said    []string
	over    bool
	changed chan struct{}
}

type testReply struct {
	ID      int          `json:"id"`
	None    bool         `json:"none"`
	Failure string       `json:"failure"`
	Errors  []string     `json:"errors"`
	Modules []testModule `json:"modules"`
}

type testModule struct {
	File   string   `json:"file"`
	Errors []string `json:"errors"`
	Tests  []struct {
		Name   string   `json:"name"`
		State  string   `json:"state"`
		Errors []string `json:"errors"`
	} `json:"tests"`
}

type testManifest struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func NewTestRunners() *TestRunners {
	return &TestRunners{running: map[string]*testRunner{}}
}

func (r *TestRunners) Close() {
	if r == nil {
		return
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for dir, runner := range r.running {
		runner.stop()
		delete(r.running, dir)
	}
}

func (r *TestRunners) Test(ctx context.Context, resolved string) (string, error) {
	since := time.Now()
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	from := resolved
	if !info.IsDir() {
		from = filepath.Dir(resolved)
	}
	dir, skipped := vitestPackage(from)
	if skipped != "" {
		return "test skipped: " + skipped, nil
	}
	runner, err := r.runnerFor(dir)
	if err != nil {
		return "", fmt.Errorf("vitest did not start in %s: %w", dir, err)
	}
	if r == nil {
		defer runner.stop()
	}
	waiting, cancel := context.WithTimeout(ctx, konst.TestRunDeadlineMillis*time.Millisecond)
	defer cancel()
	reply, err := runner.ask(waiting, resolved)
	head := fmt.Sprintf("test: vitest in %s, %d ms", dir, time.Since(since).Milliseconds())
	switch {
	case err == nil:
		return testReport(reply, dir, head), nil
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("test: vitest in %s is still running %s after %d ms. it keeps running: call test again for its answer rather than running vitest through the shell", dir, resolved, time.Since(since).Milliseconds()), nil
	case ctx.Err() != nil:
		return "test skipped: the turn was cancelled while vitest ran", nil
	}
	r.drop(dir, runner)
	return head + ", vitest stopped before it answered. its last lines:\n" + strings.Join(runner.lastSaid(), "\n"), nil
}

func vitestPackage(from string) (string, string) {
	manifestPath, found := findUp(from, "package.json")
	if !found {
		return "", "no package.json in " + from + " or above it, so run this project's tests through the shell"
	}
	dir := filepath.Dir(manifestPath)
	var manifest testManifest
	if body, err := os.ReadFile(manifestPath); err == nil {
		_ = json.Unmarshal(body, &manifest)
	}
	_, declared := manifest.DevDependencies["vitest"]
	_, depended := manifest.Dependencies["vitest"]
	command := manifest.Scripts["test"]
	usesVitest := declared || depended || strings.Contains(command, "vitest")
	_, installed := findUp(dir, "node_modules", "vitest", "package.json")
	switch {
	case !usesVitest && command == "":
		return "", manifestPath + " does not use vitest and has no test script, so find how this project runs its tests and run that through the shell"
	case !usesVitest:
		return "", "the test tool runs vitest, and " + manifestPath + " tests with " + command + ": run that through the shell"
	case !installed:
		return "", "vitest is not installed under node_modules in " + dir + " or above it: install the project's dependencies first"
	}
	return dir, ""
}

func (r *TestRunners) runnerFor(dir string) (*testRunner, error) {
	if r == nil {
		return startTestRunner(dir)
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if runner := r.running[dir]; runner != nil {
		return runner, nil
	}
	runner, err := startTestRunner(dir)
	if err == nil {
		r.running[dir] = runner
	}
	return runner, err
}

func (r *TestRunners) drop(dir string, runner *testRunner) {
	if r != nil {
		r.mutex.Lock()
		if r.running[dir] == runner {
			delete(r.running, dir)
		}
		r.mutex.Unlock()
	}
	runner.stop()
}

func startTestRunner(dir string) (*testRunner, error) {
	output, written, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(lifetime, "node", "--input-type=module", "-e", testRunnerScript)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, written, written
	cmd.Env = append(os.Environ(), "TOFU_TEST_WORKERS="+strconv.Itoa(konst.TestRunnerWorkers))
	input, err := cmd.StdinPipe()
	if err != nil {
		_, _ = output.Close(), written.Close()
		cancel()
		return nil, err
	}
	tracked, err := shell.StartTracked(cmd)
	_ = written.Close()
	if err != nil {
		_ = output.Close()
		cancel()
		return nil, err
	}
	release := sync.OnceFunc(tracked.Release)
	runner := &testRunner{process: cmd.Process, stop: func() { cancel(); release() }, input: input, answers: map[int]testReply{}, changed: make(chan struct{})}
	go runner.read(output)
	go func() {
		_ = cmd.Wait()
		release()
	}()
	return runner, nil
}

func (t *testRunner) read(output *os.File) {
	defer func() {
		_ = output.Close()
		t.settle(func() { t.over = true })
	}()
	scanner := bufio.NewScanner(output)
	scanner.Buffer(nil, konst.TestReplyBytesCap)
	for scanner.Scan() {
		line := scanner.Text()
		_, body, replied := strings.Cut(line, testReplyMarker)
		var reply testReply
		switch {
		case !replied:
			t.mutex.Lock()
			t.said = append(t.said, line)
			t.said = t.said[max(0, len(t.said)-konst.TestLinesCap):]
			t.mutex.Unlock()
		case json.Unmarshal([]byte(body), &reply) == nil:
			t.settle(func() { t.answers[reply.ID] = reply })
		}
	}
}

func (t *testRunner) settle(update func()) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	update()
	close(t.changed)
	t.changed = make(chan struct{})
}

func (t *testRunner) lastSaid() []string {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.said
}

func (t *testRunner) ask(ctx context.Context, file string) (testReply, error) {
	t.asking.Lock()
	t.asked++
	id := t.asked
	request, _ := json.Marshal(map[string]any{"id": id, "file": filepath.ToSlash(file)})
	_, err := t.input.Write(append(request, '\n'))
	t.asking.Unlock()
	if err != nil {
		return testReply{}, err
	}
	for {
		t.mutex.Lock()
		reply, answered := t.answers[id]
		delete(t.answers, id)
		over, changed := t.over, t.changed
		t.mutex.Unlock()
		switch {
		case answered:
			return reply, nil
		case over:
			return testReply{}, errors.New("vitest exited")
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return testReply{}, ctx.Err()
		}
	}
}

func testReport(reply testReply, dir, head string) string {
	if reply.Failure != "" {
		return head + ", vitest could not run it:\n" + firstLines(reply.Failure)
	}
	if reply.None {
		return head + ": no test file in this package runs it, directly or through what it imports"
	}
	var summaries, lines []string
	for _, module := range reply.Modules {
		name, _ := filepath.Rel(dir, filepath.FromSlash(module.File))
		counts := map[string]int{}
		for _, test := range module.Tests {
			counts[test.State]++
			if test.State == "failed" {
				lines = append(lines, "FAIL "+test.Name+": "+firstLines(strings.Join(test.Errors, "\n")))
			}
		}
		summary := fmt.Sprintf("%s: %d passed, %d failed", filepath.ToSlash(name), counts["passed"], counts["failed"])
		if skipped := counts["skipped"] + counts["pending"]; skipped > 0 {
			summary += fmt.Sprintf(", %d skipped", skipped)
		}
		summaries = append(summaries, summary)
		for _, failure := range module.Errors {
			lines = append(lines, filepath.ToSlash(name)+" did not run: "+firstLines(failure))
		}
	}
	for _, failure := range reply.Errors {
		lines = append(lines, "unhandled: "+firstLines(failure))
	}
	shown := lines[:min(len(lines), konst.TestLinesCap)]
	if cut := len(lines) - len(shown); cut > 0 {
		shown = append(shown, fmt.Sprintf("(%d more lines not shown)", cut))
	}
	return strings.Join(append([]string{head + ", " + strings.Join(summaries, "; ")}, shown...), "\n")
}

func firstLines(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.Join(lines[:min(len(lines), konst.TestErrorLines)], "\n")
}
