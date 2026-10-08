package shell

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestTheRegistrySaysWhyAShellWasKeptWhereItRunsWhatMadeItReadyAndHowItEnded(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shells")
	registry := OpenAt(dir)
	work := t.TempDir()
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	_ = free.Close()
	server, err := registry.YieldReady(context.Background(), shellCommand(t, work, "sleep 30"), "sleep 30", "sub-1", Wait{Within: 300 * time.Millisecond, Poll: 50 * time.Millisecond, Port: port, Kept: KeptBackground})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Kill(server.Shell.Name) })
	build, err := registry.YieldReady(context.Background(), shellCommand(t, work, "sleep 1; exit 4"), "sleep 1; exit 4", "", Wait{Within: 300 * time.Millisecond, Kept: KeptMoved})
	if err != nil {
		t.Fatal(err)
	}
	once, err := registry.YieldReady(context.Background(), shellCommand(t, work, "echo once"), "echo once", "", Wait{Within: 5 * time.Second, Kept: KeptMoved})
	if err != nil {
		t.Fatal(err)
	}
	if once.Shell.Kept != "" {
		t.Errorf("a command that ended inside its wait says it was kept as %q", once.Shell.Kept)
	}
	ended, err := OpenAt(dir).AwaitEnd(context.Background(), build.Shell.Name, 10*time.Second)
	if err != nil || ended.Kept != KeptMoved || ended.Ready != ReadyWaited || ended.ExitCode == nil || *ended.ExitCode != 4 || ended.Ended == nil {
		t.Fatalf("the moved build reads back as %+v (%v), want kept moved, ready waited, exited 4 with an end time", ended, err)
	}
	listed, _ := OpenAt(dir).List()
	if len(listed) != 2 {
		t.Fatalf("the registry lists %+v, want the server and the build and never the one-shot", listed)
	}
	running := listed[0]
	if running.Name != server.Shell.Name || running.Kept != KeptBackground || running.Ready != ReadyWaited || running.Port != port || running.Dir != work || running.LeftOver() {
		t.Fatalf("the server reads back as %+v, want kept background, ready waited, port %d, dir %s, not left over", running, port, work)
	}
}
