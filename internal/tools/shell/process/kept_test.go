package shell

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"tofu/internal/konst"
)

func TestAKeptShellsLogKeepsItsStartAndItsNewestOutputUnderTheCap(t *testing.T) {
	registry := OpenAt(filepath.Join(t.TempDir(), "shells"))
	const last = 400000
	command := "seq 1 " + strconv.Itoa(last) + "; sleep 30"
	got, err := registry.YieldReady(context.Background(), shellCommand(t, t.TempDir(), command), command, "", Wait{Within: 5 * time.Second, Kept: KeptBackground})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Kill(got.Shell.Name) })
	printed := 0
	for n := 1; n <= last; n++ {
		printed += len(strconv.Itoa(n)) + 1
	}
	log, err := os.ReadFile(registry.logPath(got.Shell.Name))
	if err != nil {
		t.Fatal(err)
	}
	marker := regexp.MustCompile(`\ntofu: (\d+) bytes dropped[^\n]*\n`).FindSubmatch(log)
	if len(log) > konst.ShellLogBytes || marker == nil || !bytes.HasPrefix(log, []byte("1\n2\n3\n")) || !bytes.HasSuffix(log, []byte(strconv.Itoa(last-1)+"\n"+strconv.Itoa(last)+"\n")) {
		t.Fatalf("the log of %d printed bytes is %d bytes, cap %d, marker %q, starts %q, ends %q", printed, len(log), konst.ShellLogBytes, marker, log[:min(12, len(log))], log[max(0, len(log)-16):])
	}
	dropped, _ := strconv.Atoi(string(marker[1]))
	if kept := len(log) - len(marker[0]); kept+dropped != printed {
		t.Fatalf("the log keeps %d bytes and says %d were dropped, which is %d, not the %d printed", kept, dropped, kept+dropped, printed)
	}
}

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
	if len(listed) != 1 {
		t.Fatalf("the registry lists %+v, want only the running server: never the ended build or the one-shot", listed)
	}
	running := listed[0]
	if running.Name != server.Shell.Name || running.Kept != KeptBackground || running.Ready != ReadyWaited || running.Port != port || running.Dir != work || running.LeftOver() {
		t.Fatalf("the server reads back as %+v, want kept background, ready waited, port %d, dir %s, not left over", running, port, work)
	}
}
