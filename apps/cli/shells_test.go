package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	shellsverb "tofu/internal/cli/verbs/shells"
	settingspkg "tofu/internal/store/settings"
	testkit "tofu/internal/testkit/cli"
	"tofu/internal/tools/shell/process"
	"tofu/internal/ui/tui/panels/shells"
)

const (
	shellsServeVar   = "TOFU_SHELLS_SERVE"
	shellsProbeVar   = "TOFU_SHELLS_PROBE"
	shellsCommandVar = "TOFU_SHELLS_COMMAND"
	shellsNameVar    = "TOFU_SHELLS_NAME"
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
	name := os.Getenv(shellsNameVar)
	registry, err := shellsverb.LaunchShellRegistry(dir)
	if err == nil {
		_, err = registry.Start(dir, name, os.Getenv(shellsCommandVar), "")
	}
	for deadline := time.Now().Add(15 * time.Second); err == nil && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		log, _ := registry.Tail(name, shell.DefaultTail)
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

func shellsProject(t *testing.T, keep bool) string {
	t.Helper()
	testkit.EmptyHome(t)
	dir := t.TempDir()
	if keep {
		store, err := settingspkg.OpenFor(dir)
		if err == nil {
			err = store.Set(settingspkg.Project, settingspkg.PersistentRegistry, 1)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func startShellsProbe(t *testing.T, dir, name string, twoLevel bool) (probe *exec.Cmd, quit io.Closer, addr string) {
	t.Helper()
	command := shellsServeVar + "=1 '" + filepath.ToSlash(os.Args[0]) + "' '-test.run=^" + t.Name() + "$'"
	if twoLevel {
		command += " & wait"
	}
	probe = exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	probe.Env = append(os.Environ(), shellsProbeVar+"="+dir, shellsCommandVar+"="+command, shellsNameVar+"="+name)
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
	return probe, stdin, addr
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
	probe, _, addr := startShellsProbe(t, shellsProject(t, false), "dev-server", false)
	if err := probe.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = probe.Wait()
	waitServerGone(t, addr, "tofu was killed with no cleanup")
}

func TestAQuitEndsAShellAndTheServerItStarted(t *testing.T) {
	runShellsHelperIfAsked()
	probe, quit, addr := startShellsProbe(t, shellsProject(t, false), "dev-server", true)
	_ = quit.Close()
	if err := probe.Wait(); err != nil {
		t.Fatalf("the probe did not quit cleanly: %v", err)
	}
	waitServerGone(t, addr, "tofu quit")
}

func TestQuittingOneTofuEndsOnlyTheShellItStarted(t *testing.T) {
	runShellsHelperIfAsked()
	dir := shellsProject(t, false)
	first, quitFirst, firstAddr := startShellsProbe(t, dir, "first", true)
	second, quitSecond, secondAddr := startShellsProbe(t, dir, "second", true)
	_ = quitFirst.Close()
	if err := first.Wait(); err != nil {
		t.Fatalf("the first tofu did not quit cleanly: %v", err)
	}
	waitServerGone(t, firstAddr, "the tofu that started it quit")
	if !serverListens(secondAddr) {
		t.Errorf("the second tofu's server on %s ended when the first tofu quit", secondAddr)
	}
	_ = quitSecond.Close()
	if err := second.Wait(); err != nil {
		t.Fatalf("the second tofu did not quit cleanly: %v", err)
	}
	waitServerGone(t, secondAddr, "the second tofu quit")
}

func TestAShellKeptPastTheQuitIsLeftOverAtTheNextLaunchAndEnds(t *testing.T) {
	runShellsHelperIfAsked()
	dir := shellsProject(t, true)
	probe, quit, addr := startShellsProbe(t, dir, "dev-server", true)
	_ = quit.Close()
	if err := probe.Wait(); err != nil {
		t.Fatalf("the probe did not quit cleanly: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if !serverListens(addr) {
		t.Fatalf("with persistentRegistry on, the server on %s ended with tofu", addr)
	}
	launch := launchOf(dir, sessionResume{}, true)
	t.Cleanup(func() { _ = launch.registry.Kill("dev-server") })
	if !strings.Contains(launch.note, "left over from an earlier tofu: 1,") {
		t.Errorf("the next launch notes %q, and never counts the shell left over", launch.note)
	}
	screen := shells.New(time.Now)
	screen.SetSize(120, 36)
	screen.Set(appShells(dir, launch.registry, nil)())
	view := ansi.Strip(screen.View())
	t.Logf("shells screen at 120x36\n%s", view)
	if !strings.Contains(view, "left over") {
		t.Errorf("the shells screen does not mark the shell left over\n%s", view)
	}
	if intent := screen.Key("k"); intent != shells.IntentKillAsk {
		t.Fatalf("k on the left over shell gives intent %d, want the confirmation", intent)
	}
	requested, _ := screen.KillRequested()
	if err := appKillShell(launch.registry, nil)(requested.Name); err != nil {
		t.Fatalf("ending the left over shell: %v", err)
	}
	waitServerGone(t, addr, "the next launch ended the left over shell")
}
