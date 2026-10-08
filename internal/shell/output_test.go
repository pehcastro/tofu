package shell

import (
	"cmp"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func startKept(t *testing.T, dir, command string) *Registry {
	t.Helper()
	registry := OpenAt(t.TempDir())
	if _, err := registry.Start(dir, "watched", command, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Kill("watched") })
	return registry
}

func tailWhile(t *testing.T, registry *Registry, until func(tail string, entry Shell) bool) (string, Shell) {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		entry, err := registry.Read("watched")
		if err != nil {
			t.Fatal(err)
		}
		tail, err := registry.Tail("watched", DefaultTail)
		if err != nil {
			t.Fatal(err)
		}
		if until(tail, entry) {
			return tail, entry
		}
	}
	t.Fatal("the shell never reached the state this test waits for")
	return "", Shell{}
}

func TestOutputReachesTailAsItIsPrinted(t *testing.T) {
	for name, command := range map[string]string{
		"bash":   "for i in 1 2 3 4; do echo line $i; sleep 1; done",
		"python": "python -c \"import time\nfor i in range(1, 5): print('line', i); time.sleep(1)\"",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := exec.LookPath(name); err != nil {
				t.Skipf("%s is not on PATH: %v", name, err)
			}
			registry := startKept(t, t.TempDir(), command)
			var seen []time.Time
			tailWhile(t, registry, func(tail string, entry Shell) bool {
				for len(seen) < 4 && strings.Contains(tail, "line "+string(rune('1'+len(seen)))) {
					if entry.State != Running {
						t.Fatalf("line %d reached Tail only after the command ended:\n%s", len(seen)+1, tail)
					}
					seen = append(seen, time.Now())
				}
				return len(seen) == 4
			})
			for i := 1; i < len(seen); i++ {
				if gap := seen[i].Sub(seen[i-1]); gap < 500*time.Millisecond {
					t.Errorf("line %d arrived %s after line %d, so they came together rather than as printed", i+1, gap, i)
				}
			}
		})
	}
}

func TestOutputNamesTheProgramThatHoldsIt(t *testing.T) {
	for command, want := range map[string]string{
		"cargo build 2>&1 | tail -2":              "tail -2",
		"for i in 1 2; do echo $i; done | grep 1": "grep",
		"ls | sort":                                       "sort",
		"make | head -5":                                  "head -5",
		"go build ./... || echo failed":                   "",
		`grep "a|b" notes.txt`:                            "",
		"make | tail -f":                                  "",
		"make | grep --line-buffered error":               "",
		"make 2>&1 | tee build.log":                       "",
		"echo 'x | tail -2'":                              "",
		"powershell -File build.ps1 -Log b.log | tail -1": "tail -1",
	} {
		got := heldBy(command, false)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%q: note %q, want one naming %q", command, got, want)
		}
	}
	for command, want := range map[string]string{
		"make | grep error":                    "",
		"make | grep error | sed s/a/b/":       "grep error",
		"make | grep -F error":                 "",
		"cargo build 2>&1 | grep -F warn | wc": "grep -F",
	} {
		got := heldBy(command, true)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("on a terminal, %q: note %q, want one naming %q", command, got, want)
		}
	}
	registry := startKept(t, t.TempDir(), "for i in 1 2 3; do echo line $i; sleep 1; done | tail -1")
	before, _ := tailWhile(t, registry, func(string, Shell) bool { return true })
	after, _ := tailWhile(t, registry, func(tail string, entry Shell) bool { return entry.State != Running })
	if !strings.Contains(before, "tail -1") {
		t.Errorf("Tail of a command piped into tail says nothing about why it is empty:\n%q", before)
	}
	if !strings.HasPrefix(after, before) || !strings.Contains(after, "line 3") {
		t.Errorf("Tail before the output arrived is not a prefix of Tail after it, so a reader's byte offset breaks:\nbefore %q\nafter  %q", before, after)
	}
}

func TestOutputFollowsACdToTheLogItsCommandNames(t *testing.T) {
	for name, into := range map[string]func(sub string) string{
		"none":     func(string) string { return "" },
		"relative": func(string) string { return `cd "a b" && ` },
		"posix": func(sub string) string {
			if runtime.GOOS == "windows" {
				sub = "/" + strings.ToLower(sub[:1]) + filepath.ToSlash(sub[2:])
			}
			return `cd "` + sub + `" && `
		},
		"twice":     func(string) string { return `cd "a b" && cd .. && cd "a b" && ` },
		"semicolon": func(string) string { return `cd 'a b'; ` },
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			sub := filepath.Join(root, "a b")
			stale := filepath.Join(root, "old.log")
			hourAgo := time.Now().Add(-time.Hour)
			if err := cmp.Or(os.Mkdir(sub, 0o755), os.WriteFile(stale, []byte("stale run\n"), 0o644), os.Chtimes(stale, hourAgo, hourAgo)); err != nil {
				t.Fatal(err)
			}
			moved := into(sub)
			if moved == "" {
				sub = root
			}
			command := moved + "for i in 1 2 3; do echo line $i >&2; sleep 1; done 2> build.log; echo --log " + filepath.ToSlash(stale)
			registry := startKept(t, root, command)
			tail, _ := tailWhile(t, registry, func(tail string, entry Shell) bool {
				if entry.State != Running {
					t.Fatalf("the named log never showed while the command ran:\n%s", tail)
				}
				return strings.Contains(tail, "line 1")
			})
			if !strings.Contains(tail, filepath.Join(sub, "build.log")) {
				t.Errorf("Tail does not name the log under the dir the command moved to:\n%s", tail)
			}
			if strings.Contains(tail, "stale run") {
				t.Errorf("Tail shows a log written an hour before this shell started:\n%s", tail)
			}
		})
	}
}

func TestOutputLastTimeFollowsTheLatestWrite(t *testing.T) {
	registry := startKept(t, t.TempDir(), "sleep 1; echo first; sleep 3; echo second; sleep 2")
	lastOutput := func() time.Time {
		entry, err := registry.Read("watched")
		if err != nil {
			t.Fatal(err)
		}
		return registry.Timing(entry, time.Now()).Last
	}
	if last := lastOutput(); !last.IsZero() {
		t.Fatalf("before anything is printed the last output is %v, want none", last)
	}
	tailWhile(t, registry, func(tail string, _ Shell) bool { return strings.Contains(tail, "first") })
	first := lastOutput()
	if first.IsZero() || time.Since(first) > time.Second {
		t.Fatalf("right after the first line the last output is %v (%s ago)", first, time.Since(first))
	}
	time.Sleep(time.Second)
	if still := lastOutput(); !still.Equal(first) {
		t.Fatalf("the last output moved from %v to %v while nothing was printed", first, still)
	}
	tailWhile(t, registry, func(tail string, _ Shell) bool { return strings.Contains(tail, "second") })
	if second := lastOutput(); second.Sub(first) < 2*time.Second || time.Since(second) > time.Second {
		t.Fatalf("after the second line the last output is %v, %s after the first", second, second.Sub(first))
	}
}

func TestOutputFollowNeverSkipsOrRepeatsAcrossTwoGrowingLogs(t *testing.T) {
	registry := startKept(t, t.TempDir(), "for i in 1 2 3 4 5 6 7 8; do echo out $i; echo log $i >> named.log; sleep 0.3; done")
	var cursor Cursor
	var stream strings.Builder
	for running := true; running; time.Sleep(30 * time.Millisecond) {
		entry, err := registry.Read("watched")
		if err != nil {
			t.Fatal(err)
		}
		running = entry.State == Running
		text, err := registry.Follow("watched", &cursor)
		if err != nil {
			t.Fatal(err)
		}
		stream.WriteString(text)
	}
	joined := stream.String()
	for _, source := range []string{"out", "log"} {
		at := 0
		for i := 1; i <= 8; i++ {
			line := source + " " + string(rune('0'+i)) + "\n"
			if count := strings.Count(joined, line); count != 1 {
				t.Fatalf("%q arrived %d times, want once:\n%s", line, count, joined)
			}
			next := strings.Index(joined, line)
			if next < at {
				t.Fatalf("%q arrived before the line printed ahead of it:\n%s", line, joined)
			}
			at = next
		}
	}
}

func TestOutputFollowKeepsALetterSplitAcrossReadsAndARewrittenLog(t *testing.T) {
	dir := t.TempDir()
	named := filepath.Join(dir, "grow.log")
	registry := startKept(t, dir, "sleep 10; echo --log grow.log")
	var cursor Cursor
	follow := func(raw []byte, appended bool) string {
		t.Helper()
		flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		if appended {
			flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
		}
		file, err := os.OpenFile(named, flags, 0o644)
		if err == nil {
			_, err = file.Write(raw)
			err = cmp.Or(err, file.Close())
		}
		if err != nil {
			t.Fatal(err)
		}
		text, err := registry.Follow("watched", &cursor)
		if err != nil {
			t.Fatal(err)
		}
		return text
	}
	first := follow([]byte("caf\xc3"), true)
	second := follow([]byte("\xa9\n"), true)
	if joined := first + second; strings.ContainsRune(joined, '�') || !strings.Contains(joined, "café\n") {
		t.Fatalf("a letter split across two reads came back as %q then %q", first, second)
	}
	if rewritten := follow([]byte("a new start\n"), false); !strings.Contains(rewritten, "a new start\n") {
		t.Fatalf("a log rewritten from empty came back as %q", rewritten)
	}
}

func TestOutputTailReadsTheEndOfALongLog(t *testing.T) {
	registry := startKept(t, t.TempDir(), "seq 1 200000; sleep 10")
	tail, _ := tailWhile(t, registry, func(tail string, _ Shell) bool { return strings.HasSuffix(tail, "200000") })
	if tail != "199998\n199999\n200000" && !strings.HasSuffix(tail, "\n199998\n199999\n200000") {
		t.Fatalf("Tail does not end with the last lines: %q", tail[max(0, len(tail)-40):])
	}
	whole, err := registry.Tail("watched", 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(whole, "\n")
	if len(lines) < 1000 {
		t.Fatalf("Tail of every line returned %d lines", len(lines))
	}
	for i, line := range lines[1:] {
		if strings.TrimLeft(line, "0123456789") != "" || len(lines[i]) > len(line) {
			t.Fatalf("line %d of Tail is %q after %q, so the read cut a line", i+1, line, lines[i])
		}
	}
	started := time.Now()
	for range 20 {
		_, _ = registry.Tail("watched", DefaultTail)
	}
	t.Logf("Tail of a %d-line log: %s a read", len(lines), time.Since(started)/20)
}

func TestOutputAForegroundCommandIsListedWhileItRuns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shells")
	registry := OpenAt(dir)
	command := "for i in 1 2 3; do echo fg $i; sleep 1; done"
	done := make(chan Yielded, 1)
	go func() {
		got, _ := registry.YieldReady(t.Context(), shellCommand(t, t.TempDir(), command), command, "lead", Wait{Within: 10 * time.Second})
		done <- got
	}()
	seen := ""
	for deadline := time.Now().Add(2 * time.Second); seen == "" && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		listed, _ := registry.List()
		for _, one := range listed {
			if tail, _ := registry.Tail(one.Name, DefaultTail); one.State == Running && strings.Contains(tail, "fg 1") {
				seen = one.Name
			}
		}
	}
	if seen == "" {
		t.Fatal("a foreground command printing once a second was not listed with its output within 2 s")
	}
	if got := <-done; got.Ready != ReadyExited || !strings.Contains(got.Output, "fg 3") {
		t.Fatalf("the command came back %+v", got)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Fatalf("a command that ended inside the wait left %d files in the registry", len(left))
	}
}
