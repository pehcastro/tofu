package shell

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"tofu/internal/konst"
)

func TestAServerThatPrintsItsURLReturnsAsSoonAsItSaysSo(t *testing.T) {
	command := "sleep 0.2; echo listening on http://localhost:4321; sleep 30"
	registry := OpenAt(filepath.Join(t.TempDir(), "shells"))
	started := time.Now()
	got, err := registry.YieldReady(context.Background(), shellCommand(t, t.TempDir(), command), command, "", Wait{Within: konst.BackgroundYieldMillis * time.Millisecond, Poll: konst.PortCheckTimeoutMillis * time.Millisecond})
	took := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Kill(got.Shell.Name) })
	if got.Shell.State != Running || got.Ready != ReadyLine || took >= time.Second {
		t.Fatalf("a server printing its url after 200ms came back %s after %v as %+v, want running on a ready line under 1s", got.Ready, took, got.Shell)
	}
}
