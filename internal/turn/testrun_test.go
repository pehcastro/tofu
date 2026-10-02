package turn

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func vitestProject(t *testing.T) string {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun is not on PATH")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"package.json": `{"name":"tested","type":"module","scripts":{"test":"vitest run"},"devDependencies":{"vitest":"3.2.4"}}`,
		"sum.ts":       "export const sum = (a: number, b: number) => a + b;\n",
		"sum.test.ts":  "import { expect, it } from 'vitest';\nimport { sum } from './sum';\n\nit('adds', () => {\n  expect(sum(1, 2)).toBe(3);\n});\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	install := exec.Command("bun", "install", "--prefer-offline")
	install.Dir = dir
	if out, err := install.CombinedOutput(); err != nil {
		t.Skipf("vitest could not be installed from the bun cache: %v\n%s", err, out)
	}
	return dir
}

func onlyRunner(t *testing.T, runners *TestRunners) *testRunner {
	runners.mutex.Lock()
	defer runners.mutex.Unlock()
	if len(runners.running) != 1 {
		t.Fatalf("%d test runners are running, want one", len(runners.running))
	}
	for _, runner := range runners.running {
		return runner
	}
	return nil
}

func TestASecondTestRunAfterAnEditIsAnsweredFasterByTheSameRunner(t *testing.T) {
	dir := vitestProject(t)
	runners := NewTestRunners()
	t.Cleanup(runners.Close)
	started := time.Now()
	first, err := runners.Test(context.Background(), filepath.Join(dir, "sum.test.ts"))
	firstTook := time.Since(started)
	if err != nil || !strings.Contains(first, "sum.test.ts: 1 passed, 0 failed") {
		t.Fatalf("the first run did not report the file passing: %v\n%s", err, first)
	}
	runner := onlyRunner(t, runners)
	if err := os.WriteFile(filepath.Join(dir, "sum.ts"), []byte("export const sum = (a: number, b: number) => a - b;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	second, err := runners.Test(context.Background(), filepath.Join(dir, "sum.ts"))
	secondTook := time.Since(started)
	if err != nil || !strings.Contains(second, "sum.test.ts: 0 passed, 1 failed") || !strings.Contains(second, "expected -1 to be 3") {
		t.Fatalf("the run after the edit did not report the failure:  %v\n%s", err, second)
	}
	if onlyRunner(t, runners) != runner || secondTook >= firstTook {
		t.Fatalf("the second run took %s against the first's %s, or came from another runner", secondTook, firstTook)
	}
	t.Logf("first, %s:\n%s\nsecond, %s:\n%s", firstTook, first, secondTook, second)
}

func TestAProjectWithoutVitestNamesItsOwnTestCommand(t *testing.T) {
	for _, project := range []struct{ manifest, want string }{
		{`{"name":"jested","scripts":{"test":"jest --ci"}}`, "jest --ci"},
		{`{"name":"bare"}`, "has no test script"},
		{`{"name":"uninstalled","devDependencies":{"vitest":"3.2.4"}}`, "vitest is not installed"},
	} {
		dir := t.TempDir()
		for name, body := range map[string]string{"package.json": project.manifest, "a.test.ts": "export {};\n"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		got, err := NewTestRunners().Test(context.Background(), filepath.Join(dir, "a.test.ts"))
		if err != nil || !strings.Contains(got, project.want) {
			t.Fatalf("%s: want the fallback naming %q, got %v\n%s", project.manifest, project.want, err, got)
		}
	}
}

func TestClosingTheRunnersLeavesNoVitestProcess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the process tree is read from Win32_Process")
	}
	dir := vitestProject(t)
	runners := NewTestRunners()
	if _, err := runners.Test(context.Background(), filepath.Join(dir, "sum.test.ts")); err != nil {
		t.Fatal(err)
	}
	pid := strconv.Itoa(onlyRunner(t, runners).process.Pid)
	alive := func() string {
		out, err := exec.Command("powershell", "-NoProfile", "-Command",
			"@(Get-CimInstance Win32_Process -Filter 'ProcessId="+pid+" or ParentProcessId="+pid+"').Count").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	before := alive()
	runners.Close()
	time.Sleep(time.Second)
	if after := alive(); after != "0" {
		t.Fatalf("after Close, %s of the runner's %s processes are still alive", after, before)
	}
	t.Logf("the runner and its workers were %s processes, none left after Close", before)
}

func TestASourceFileRunsOnlyTheTestBesideIt(t *testing.T) {
	dir := vitestProject(t)
	for name, body := range map[string]string{
		"uses.test.ts":  "import { expect, it } from 'vitest';\nimport { sum } from './sum';\n\nit('uses', () => {\n  expect(sum(2, 2)).toBe(4);\n});\n",
		"sumx.test.ts":  "import { it } from 'vitest';\n\nit('collides', () => {});\n",
		"sum-a.test.ts": "import { it } from 'vitest';\n\nit('collides', () => {});\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runners := NewTestRunners()
	t.Cleanup(runners.Close)
	got, err := runners.Test(context.Background(), filepath.Join(dir, "sum.ts"))
	first, _, _ := strings.Cut(got, "\n")
	if err != nil || !strings.Contains(first, "the tests beside sum.ts, sum.test.ts") || !strings.Contains(got, "sum.test.ts: 1 passed, 0 failed") {
		t.Fatalf("the first line does not name the colocated test it ran: %v\n%s", err, got)
	}
	for _, other := range []string{"uses.test.ts", "sumx.test.ts", "sum-a.test.ts"} {
		if strings.Contains(got, other+":") {
			t.Fatalf("%s ran for sum.ts:\n%s", other, got)
		}
	}
	t.Log(got)
}

func TestASourceFileWithNoTestBesideItAnswersWithoutVitest(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"package.json":                     `{"name":"tested","devDependencies":{"vitest":"3.2.4"}}`,
		"node_modules/vitest/package.json": `{"name":"vitest"}`,
		"lonely.ts":                        "export const lonely = 1;\n",
		"lonelier.test.ts":                 "export {};\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runners := NewTestRunners()
	t.Cleanup(runners.Close)
	got, err := runners.Test(context.Background(), filepath.Join(dir, "lonely.ts"))
	if err != nil || !strings.Contains(got, "lonely.test.ts") || len(runners.running) != 0 {
		t.Fatalf("want an answer naming lonely.test.ts and no runner, got %d runners, %v\n%s", len(runners.running), err, got)
	}
	t.Log(got)
}

func TestAMissedDeadlineLeavesTheNextCallANewRunner(t *testing.T) {
	dir := vitestProject(t)
	hang := "import { it } from 'vitest';\n\nit('hangs', () => new Promise(() => {}), 3600000);\n"
	if err := os.WriteFile(filepath.Join(dir, "hang.test.ts"), []byte(hang), 0o600); err != nil {
		t.Fatal(err)
	}
	runners := NewTestRunners()
	t.Cleanup(runners.Close)
	waiting, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	missed, err := runners.Test(waiting, filepath.Join(dir, "hang.test.ts"))
	if err != nil || !strings.Contains(missed, "stopped") || strings.Contains(missed, "call test again") || len(runners.running) != 0 {
		t.Fatalf("the miss did not stop and drop its runner, %d runners left, %v\n%s", len(runners.running), err, missed)
	}
	started := time.Now()
	next, err := runners.Test(context.Background(), filepath.Join(dir, "sum.test.ts"))
	if err != nil || !strings.Contains(next, "sum.test.ts: 1 passed, 0 failed") || onlyRunner(t, runners) == nil {
		t.Fatalf("the next call did not get an answer from a new runner: %v\n%s", err, next)
	}
	t.Logf("miss:\n%s\nnext, %s:\n%s", missed, time.Since(started), next)
}
