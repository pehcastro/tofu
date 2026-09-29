package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	settingspkg "tofu/internal/settings"
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

const (
	shellsServeVar   = "TOFU_SHELLS_SERVE"
	shellsProbeVar   = "TOFU_SHELLS_PROBE"
	shellsCommandVar = "TOFU_SHELLS_COMMAND"
	serverLifetime   = 30 * time.Second
	listeningPrefix  = "listening on http://"
)

func runShellsHelperIfAsked() {
	if os.Getenv(shellsServeVar) != "" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fmt.Println("server:", err)
			os.Exit(1)
		}
		fmt.Println(listeningPrefix + listener.Addr().String())
		time.Sleep(serverLifetime)
		os.Exit(0)
	}
	dir := os.Getenv(shellsProbeVar)
	if dir == "" {
		return
	}
	registry, err := launchShellRegistry(dir)
	if err == nil {
		_, err = registry.Start(dir, "dev-server", os.Getenv(shellsCommandVar), "")
	}
	for deadline := time.Now().Add(15 * time.Second); err == nil && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		log, _ := registry.Tail("dev-server", shell.DefaultTail)
		if _, addr, found := strings.Cut(log, listeningPrefix); found {
			fmt.Println(strings.Fields(addr)[0])
			_, _ = io.Copy(io.Discard, os.Stdin)
			leaveShells(registry)
			os.Exit(0)
		}
	}
	fmt.Println("probe: the server never listened:", err)
	os.Exit(1)
}

func startShellsProbe(t *testing.T, keep, twoLevel bool) (probe *exec.Cmd, quit io.Closer, dir, addr string) {
	t.Helper()
	emptyHome(t)
	dir = t.TempDir()
	if keep {
		store, err := openSettings(dir)
		if err == nil {
			err = store.Set(settingspkg.Project, settingspkg.PersistentRegistry, 1)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	command := shellsServeVar + "=1 '" + filepath.ToSlash(os.Args[0]) + "' '-test.run=^" + t.Name() + "$'"
	if twoLevel {
		command += " & wait"
	}
	probe = exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	probe.Env = append(os.Environ(), shellsProbeVar+"="+dir, shellsCommandVar+"="+command)
	stdin, err := probe.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := probe.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := probe.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(stdout).ReadString('\n')
	addr = strings.TrimSpace(line)
	if !serverListens(addr) {
		_ = probe.Process.Kill()
		t.Fatalf("the probe's server is not listening: %q", addr)
	}
	return probe, stdin, dir, addr
}

func serverListens(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
	}
	return err == nil
}

func waitServerGone(t *testing.T, addr, after string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if !serverListens(addr) {
			return
		}
	}
	t.Errorf("the server tofu started still listens on %s after %s", addr, after)
}

func TestAShellEndsWhenTofuIsKilledHard(t *testing.T) {
	runShellsHelperIfAsked()
	probe, _, _, addr := startShellsProbe(t, false, false)
	if err := probe.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = probe.Wait()
	waitServerGone(t, addr, "tofu was killed with no cleanup")
}

func TestAQuitEndsAShellAndTheServerItStarted(t *testing.T) {
	runShellsHelperIfAsked()
	probe, quit, _, addr := startShellsProbe(t, false, true)
	_ = quit.Close()
	if err := probe.Wait(); err != nil {
		t.Fatalf("the probe did not quit cleanly: %v", err)
	}
	waitServerGone(t, addr, "tofu quit")
}

func TestWithPersistentRegistryOnAShellOutlivesTheQuit(t *testing.T) {
	runShellsHelperIfAsked()
	probe, quit, dir, addr := startShellsProbe(t, true, true)
	_ = quit.Close()
	if err := probe.Wait(); err != nil {
		t.Fatalf("the probe did not quit cleanly: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if !serverListens(addr) {
		t.Errorf("with persistentRegistry on, the server on %s ended with tofu", addr)
	}
	next, err := launchShellRegistry(dir)
	if err == nil {
		err = next.Kill("dev-server")
	}
	if err != nil {
		t.Fatalf("the next launch could not stop the kept shell: %v", err)
	}
	waitServerGone(t, addr, "the next launch stopped it")
}
