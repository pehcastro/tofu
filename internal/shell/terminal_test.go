package shell

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTerminalOnlyWhereAFilterEndsAPipeline(t *testing.T) {
	for command, want := range map[string]bool{
		"make | grep error": true,
		"for i in 1 2; do echo $i; sleep 1; done | sed s/x/y/": true,
		"cd sub && make 2>&1 | awk '{print $1}'":               true,
		"go vet ./...; make | cut -c1-80":                      true,
		"make | grep error | tail -1":                          false,
		"grep -r needle .":                                     false,
		"make | grep --line-buffered error":                    false,
		"make | sed -u s/a/b/":                                 false,
		`echo "a | grep b"`:                                    false,
		"make || grep x notes":                                 false,
		"make | grep error > errors.txt":                       false,
		"make | tail -2":                                       false,
	} {
		if got := endsInFilter(command); got != want {
			t.Errorf("%q: terminal %v, want %v", command, got, want)
		}
	}
}

func TestTerminalTextFromTheConsole(t *testing.T) {
	wide := strings.Repeat("a", consoleColumns)
	stream := "\x1b[2J\x1b[m\x1b[Hline 1\r\n\x1b]0;C:\\Program Files\\Git\\bin\\bash.exe\a\x1b[?25h" +
		"\x1b[8CTAB\r\n" +
		"\x1b[31mred\x1b[m\r\n" +
		wide + "\r\n" + "bc\x1b[K\r\n" +
		"short\r\n" +
		"caf\xc3\xa9 \xe4\xb8\xad\r\n" +
		"no newline"
	want := "line 1\n        TAB\nred\n" + wide + "bc\nshort\ncafé 中\nno newline"
	whole := &bytes.Buffer{}
	_, _ = (&screenText{out: whole}).Write([]byte(stream))
	if whole.String() != want {
		t.Errorf("in one read:\n got %q\nwant %q", whole.String(), want)
	}
	split := &bytes.Buffer{}
	screen := &screenText{out: split}
	for i := range len(stream) {
		_, _ = screen.Write([]byte{stream[i]})
	}
	if split.String() != want {
		t.Errorf("one byte a read:\n got %q\nwant %q", split.String(), want)
	}
}

func onATerminalHost(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("a pseudo terminal is opened on Windows and Linux only")
	}
}

func TestTerminalStreamsAFilterAsItPrints(t *testing.T) {
	onATerminalHost(t)
	for name, command := range map[string]string{
		"grep": "for i in 1 2 3 4; do echo line $i; sleep 1; done | grep line",
		"sed":  "for i in 1 2 3 4; do echo line $i; sleep 1; done | sed s/x/y/",
	} {
		t.Run(name, func(t *testing.T) {
			registry := startKept(t, t.TempDir(), command)
			var seen []time.Time
			_, entry := tailWhile(t, registry, func(tail string, entry Shell) bool {
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
			raw, _ := os.ReadFile(registry.logPath("watched"))
			if !entry.Terminal || bytes.ContainsAny(raw, "\x1b\r") {
				t.Errorf("terminal %v, log %q: want a terminal and plain lines", entry.Terminal, raw)
			}
		})
	}
}

func TestTerminalKeepsTheExitCodeTheLastLineAndStdin(t *testing.T) {
	onATerminalHost(t)
	long := strings.Repeat("0", 2999) + "7"
	command := "printf '%03000d\\n' 7 | grep 7; grep x; echo last | sed s/l/L/; exit 3"
	started := time.Now()
	got, err := OpenAt(t.TempDir()).YieldReady(t.Context(), shellCommand(t, t.TempDir(), command), command, "lead", Wait{Within: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(started)
	if got.Ready != ReadyExited || *got.Shell.ExitCode != 3 || !got.Shell.Terminal || took > 5*time.Second {
		t.Fatalf("ready %q, code %v, terminal %v, took %s: want an exit 3 under a terminal well inside the wait", got.Ready, *got.Shell.ExitCode, got.Shell.Terminal, took)
	}
	if got.Output != long+"\nLast\n" {
		t.Errorf("output %q, want the 3000 character line whole and then Last", got.Output)
	}
}

func TestTerminalShellStopEndsItsTree(t *testing.T) {
	onATerminalHost(t)
	registry := startKept(t, t.TempDir(), "(echo up; sleep 30; echo x) | grep x")
	started := time.Now()
	if err := registry.Kill("watched"); err != nil {
		t.Fatal(err)
	}
	entry, _ := registry.Read("watched")
	if took := time.Since(started); took > 3*time.Second || entry.State != Killed || treeAlive(entry.PID) {
		t.Fatalf("stop took %s, state %s, tree alive %v", took, entry.State, treeAlive(entry.PID))
	}
}

func TestADeadlineKillSaysSoAndAStopDoesNot(t *testing.T) {
	registry := OpenAt(t.TempDir())
	command := "echo before; sleep 30"
	got, err := registry.YieldReady(t.Context(), shellCommand(t, t.TempDir(), command), command, "lead", Wait{Within: time.Second})
	if err != nil || got.Ready != ReadyWaited {
		t.Fatalf("ready %q, err %v", got.Ready, err)
	}
	if err := registry.KillAtDeadline(got.Shell.Name, 1500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	entry, _ := registry.Read(got.Shell.Name)
	tail, _ := registry.Tail(got.Shell.Name, DefaultTail)
	if entry.State != Killed || !strings.HasPrefix(tail, "before") || !strings.HasSuffix(tail, "tofu: hit the deadline after 1.5 s and was killed") {
		t.Errorf("state %s, tail %q", entry.State, tail)
	}
	stopped := startKept(t, t.TempDir(), command)
	tailWhile(t, stopped, func(tail string, _ Shell) bool { return strings.Contains(tail, "before") })
	_ = stopped.Kill("watched")
	if tail, _ := stopped.Tail("watched", DefaultTail); strings.Contains(tail, "deadline") {
		t.Errorf("a shell a person stopped reads as a deadline: %q", tail)
	}
	for after, want := range map[time.Duration]string{1500 * time.Millisecond: "hit the deadline after 1.5 s", 600 * time.Second: "hit the deadline after 600 s"} {
		if got := HitDeadline(after); got != want {
			t.Errorf("%s: %q, want %q", after, got, want)
		}
	}
}
