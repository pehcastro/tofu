package shell

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAShellThatCannotBeListedLeavesNothingRunningOrHoldingItsLog(t *testing.T) {
	for name, command := range map[string]string{
		"console": "ping -n 30 127.0.0.1 | grep never",
		"pipes":   "ping -n 30 127.0.0.1",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "BASH-1.json"), dirMode); err != nil {
				t.Fatal(err)
			}
			registry := OpenAt(dir)
			registry.Lifetime = OutlivesTofu
			choice, err := Resolve("")
			if err != nil {
				t.Fatal(err)
			}
			cmd := choice.Command(context.Background(), t.TempDir(), command)
			if len(cmd.Args) != 3 || cmd.Args[1] != "-c" {
				t.Skipf("%s does not take a command after -c", choice.Label)
			}
			if _, err := registry.YieldReady(t.Context(), cmd, command, "", Wait{Within: 5 * time.Second}); err == nil {
				t.Fatal("bash-1 was listed over a folder of the same name")
			}
			pid := cmd.Process.Pid
			t.Cleanup(func() { _ = killTree(pid) })
			log := filepath.Join(dir, "bash-1"+logSuffix)
			for deadline := time.Now().Add(killWait); os.Remove(log) != nil; time.Sleep(50 * time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatalf("%s is still held open %s after the listing failed: %v", log, killWait, os.Remove(log))
				}
			}
		})
	}
}
