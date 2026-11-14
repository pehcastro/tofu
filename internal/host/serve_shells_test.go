package host

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"tofu/internal/tools/shell/process"
)

func TestServeAnnouncesOnlyAKeptShellAndClosesEveryNameItAnnounced(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shells")
	registry := process.OpenAt(dir)
	c, _, _ := serving(t, nil, ServeConfig{Shells: registry})
	c.ask("1", "initialize", `{"client":"desk"}`)
	c.answer("1", &InitializeResult{})
	choice, err := process.Resolve(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	run := func(command string, wait process.Wait) process.Shell {
		cmd := exec.Command(choice.Path, "-c", command)
		cmd.Dir = work
		got, err := registry.YieldReady(context.Background(), cmd, command, "lead", wait)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = registry.Kill(got.Shell.Name) })
		return got.Shell
	}
	var started []ShellStarted
	seen := func(method, name string) func(wireLine) bool {
		return func(line wireLine) bool {
			if line.Method == "shell.started" {
				var one ShellStarted
				_ = json.Unmarshal(line.Params, &one)
				started = append(started, one)
			}
			var named struct{ Shell string }
			_ = json.Unmarshal(line.Params, &named)
			return line.Method == method && named.Shell == name
		}
	}
	exited := func(name string) ShellExited {
		var one ShellExited
		_ = json.Unmarshal(c.until(seen("shell.exited", name), "shell.exited for "+name).Params, &one)
		return one
	}

	gone := run("sleep 30", process.Wait{Within: 200 * time.Millisecond, Kept: process.KeptMoved})
	c.until(seen("shell.started", gone.Name), "shell.started for "+gone.Name)
	listed := filepath.Join(dir, "running", gone.Name+".json")
	if err := os.Rename(listed, listed+".away"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(listed+".away", listed) })
	if vanished := exited(gone.Name); vanished.EndedAt == nil {
		t.Errorf("the shell gone from the list closed as %+v, want an end time", vanished)
	}

	once := run("sleep 1", process.Wait{Within: 5 * time.Second, Kept: process.KeptMoved})
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	_ = free.Close()
	server := run("sleep 2; exit 5", process.Wait{Within: 300 * time.Millisecond, Poll: 50 * time.Millisecond, Port: port, Kept: process.KeptBackground})
	c.until(seen("shell.started", server.Name), "shell.started for "+server.Name)
	if ended := exited(server.Name); ended.ExitCode == nil || *ended.ExitCode != 5 || ended.EndedAt == nil {
		t.Errorf("the background shell closed as %+v, want exit 5 with an end time", ended)
	}
	for _, one := range started {
		if one.Shell == once.Name {
			t.Errorf("the one-shot %s was announced as a shell: %+v", once.Name, one)
		}
		if one.Shell == server.Name && (one.Kept != ShellKept(process.KeptBackground) || one.Dir != work || one.Port != port || one.Ready != ShellReady(process.ReadyWaited) || one.LeftOver) {
			t.Errorf("the background shell was announced as %+v, want kept background, dir %s, port %d, ready waited", one, work, port)
		}
	}
}
