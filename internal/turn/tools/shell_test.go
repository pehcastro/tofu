package tools_test

import (
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"tofu/internal/shell"
	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitPort(t *testing.T, port int, open bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for shell.PortOpen("127.0.0.1", port, 200*time.Millisecond) != open {
		if time.Now().After(deadline) {
			t.Fatalf("port %d never became open=%v", port, open)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func serving(t *testing.T) (*shell.Registry, context.Context, shell.Shell, int) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH, so no server can be started")
	}
	registry := shell.OpenAt(filepath.Join(t.TempDir(), "shells"))
	port := freePort(t)
	command := "node -e \"require('http').createServer((q,s)=>s.end('ok')).listen(" + strconv.Itoa(port) + ")\""
	started, err := registry.Start(t.TempDir(), "bash-1", command, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Kill("bash-1") })
	waitPort(t, port, true)
	return registry, turn.WithShellRegistry(context.Background(), registry), started, port
}

func runShell(t *testing.T, ctx context.Context, op, name string) turn.Result {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"op": op, "name": name})
	result, err := tools.Shells{}.Run(ctx, raw)
	if err != nil {
		t.Fatalf("shell %s %s: %v", op, name, err)
	}
	return result
}

func TestStopKillsTheServerTheWrapperStartedAndRestartBringsItBack(t *testing.T) {
	registry, ctx, started, port := serving(t)
	runShell(t, ctx, "stop", "bash-1")
	if shell.PortOpen("127.0.0.1", port, 500*time.Millisecond) {
		t.Fatalf("port %d still answers after stop, so the node child outlived its wrapper", port)
	}
	restarted := runShell(t, ctx, "restart", "bash-1")
	waitPort(t, port, true)
	again, err := registry.Read("bash-1")
	if err != nil || again.State != shell.Running || again.Dir != started.Dir || again.Command != started.Command || again.PID == started.PID {
		t.Fatalf("after restart the entry reads %+v, err %v, want the same command and folder under a new pid\n%s", again, err, restarted.Content)
	}
}

func TestAKillOfAShellPidIsRoutedToStopAndAStrangerIsNot(t *testing.T) {
	registry, ctx, started, port := serving(t)
	for _, stranger := range []string{
		"taskkill //PID 999999 //F",
		"netstat -ano | grep 8799 | awk '{print $5}' | xargs taskkill //F //PID",
		"taskkill //IM node.exe //F",
		"echo " + strconv.Itoa(started.PID),
	} {
		if owned := registry.Owning(stranger); len(owned) != 0 {
			t.Errorf("%q was read as a kill of %+v", stranger, owned)
		}
	}
	for _, own := range []string{
		"taskkill //PID " + strconv.Itoa(started.PID) + " //F",
		"kill -9 " + strconv.Itoa(started.PID),
		"pkill -P " + strconv.Itoa(started.PID),
	} {
		if owned := registry.Owning(own); len(owned) != 1 || owned[0].Name != "bash-1" {
			t.Errorf("%q was not read as a kill of bash-1: %+v", own, owned)
		}
	}
	bash, err := turn.NewBashTool(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"command": "taskkill //PID " + strconv.Itoa(started.PID) + " //F"})
	result, err := bash.Run(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if shell.PortOpen("127.0.0.1", port, 500*time.Millisecond) {
		t.Fatalf("port %d still answers after a routed taskkill\n%s", port, result.Content)
	}
	if after, _ := registry.Read("bash-1"); after.State != shell.Killed {
		t.Fatalf("the routed taskkill left bash-1 %s, want killed\n%s", after.State, result.Content)
	}
	if owned := registry.Owning("taskkill //PID " + strconv.Itoa(started.PID) + " //F"); len(owned) != 0 {
		t.Fatalf("a stopped shell's pid still reads as owned: %+v", owned)
	}
}

func TestTheShellToolNeedsARegistryAndAKnownOp(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{"op": "stop", "name": "bash-1"})
	if _, err := (tools.Shells{}).Run(context.Background(), raw); err == nil {
		t.Fatal("stop ran with no registry attached to the turn")
	}
	_, ctx, _, _ := serving(t)
	raw, _ = json.Marshal(map[string]string{"op": "pause", "name": "bash-1"})
	if _, err := (tools.Shells{}).Run(ctx, raw); err == nil {
		t.Fatal("an unknown op ran")
	}
	raw, _ = json.Marshal(map[string]string{"op": "stop", "name": "bash-9"})
	if _, err := (tools.Shells{}).Run(ctx, raw); err == nil {
		t.Fatal("stop of a shell nobody started succeeded")
	}
}
