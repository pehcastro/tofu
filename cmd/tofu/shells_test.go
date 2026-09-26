package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"tofu/internal/shell"
)

func TestShellsVerbListsReadsAndKillsARealProcess(t *testing.T) {
	registry := shell.OpenAt(t.TempDir())
	root := t.TempDir()
	if _, err := registry.Start(root, "dev-server", "sleep 10 & echo listening on :3000; wait", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Start(root, "build", "echo compiling && echo done", ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if entry, err := registry.Read("build"); err == nil && entry.State == shell.Exited {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	var list, log, kill bytes.Buffer
	if code := shellsList(registry, &list, &list); code != exitOK {
		t.Fatalf("shellsList exited %d\n%s", code, list.String())
	}
	t.Logf("tofu shells list\n%s", list.String())
	for _, want := range []string{"dev-server", "running", "build", "exited", "echo compiling"} {
		if !strings.Contains(list.String(), want) {
			t.Errorf("tofu shells list does not show %q\n%s", want, list.String())
		}
	}

	if code := shellsLog(registry, []string{"build"}, &log, &log); code != exitOK {
		t.Fatalf("shellsLog exited %d\n%s", code, log.String())
	}
	t.Logf("tofu shells log build\n%s", log.String())
	if !strings.Contains(log.String(), "compiling") || !strings.Contains(log.String(), "done") {
		t.Errorf("tofu shells log build does not show its output\n%s", log.String())
	}

	if code := shellsKill(registry, []string{"dev-server"}, &kill, &kill); code != exitOK {
		t.Fatalf("shellsKill exited %d\n%s", code, kill.String())
	}
	t.Logf("tofu shells kill dev-server\n%s", kill.String())
	if !strings.Contains(kill.String(), "dev-server killed") {
		t.Errorf("tofu shells kill dev-server does not confirm it\n%s", kill.String())
	}

	entry, err := registry.Read("dev-server")
	if err != nil {
		t.Fatal(err)
	}
	if entry.State != shell.Killed {
		t.Errorf("the process killed from the command line is state %q, want %q", entry.State, shell.Killed)
	}
}
