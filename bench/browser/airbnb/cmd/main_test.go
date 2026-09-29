package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/bench/browser/airbnb"
)

func TestARunPastItsCapIsEndedWithItsChildAndTheRowReadsCapped(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv(fakeTofuSleeper, pidFile)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	capped, err := tofuCapped(self, t.TempDir(), io.Discard, time.Second, "run")
	took := time.Since(started)
	t.Logf("capped %v, err %v, after %s", capped, err, took.Round(time.Millisecond))
	if !capped || took > 15*time.Second {
		t.Fatalf("a fake tofu sleeping %s under a 1 s cap ended capped=%v after %s", fakeTofuNap, capped, took)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(raw))
	for deadline := time.Now().Add(10 * time.Second); processAlive(pid); time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the fake tofu's child %d outlived the cap", pid)
		}
	}
	table, err := airbnb.Render([]airbnb.Row{airbnb.Score(airbnb.Task{}, airbnb.Run{Arm: airbnb.ArmB1, Capped: capped})})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(table, "| B1 | as set, capped |") {
		t.Errorf("the row does not read capped:\n%s", table)
	}
}

const (
	fakeTofuLog     = "AIRBNB_BENCH_FAKE_TOFU_LOG"
	fakeTofuSleeper = "AIRBNB_BENCH_FAKE_TOFU_SLEEPS_AND_WRITES_ITS_CHILD_PID_TO"
	fakeTofuChild   = "AIRBNB_BENCH_FAKE_TOFU_CHILD"
	fakeTofuNap     = time.Minute
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeTofuChild) != "" {
		time.Sleep(fakeTofuNap)
		os.Exit(0)
	}
	if pidFile := os.Getenv(fakeTofuSleeper); pidFile != "" {
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), fakeTofuChild+"=1", fakeTofuSleeper+"=")
		if child.Start() != nil || os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o644) != nil {
			os.Exit(2)
		}
		time.Sleep(fakeTofuNap)
		os.Exit(0)
	}
	if log := os.Getenv(fakeTofuLog); log != "" {
		file, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			_, err = fmt.Fprintln(file, strings.Join(os.Args[1:], " "))
		}
		if err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestAnArmWritesItsBrowserModelIntoTheProjectSettingsOnly(t *testing.T) {
	for _, arm := range []struct {
		arm  airbnb.Arm
		want string
	}{
		{airbnb.ArmB3, "settings set --scope project browserModel codex-sub/gpt-5.6-sol"},
		{airbnb.ArmB4, "settings set --scope project browserModel codex-sub/gpt-5.6-luna"},
		{airbnb.BrowserArm("x/y"), "settings set --scope project browserModel x/y"},
	} {
		log := filepath.Join(t.TempDir(), "tofu-args.log")
		t.Setenv(fakeTofuLog, log)
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		if err := configure(arm.arm, func(args ...string) error { return tofuAt(self, t.TempDir(), os.Stdout, args...) }); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		said := strings.Split(strings.TrimSpace(string(raw)), "\n")
		t.Logf("arm %s ran tofu with:\n%s", arm.arm, raw)
		if !strings.Contains(string(raw), arm.want+"\n") || !strings.Contains(string(raw), "browserDriver subagent\n") {
			t.Errorf("arm %s wrote %q, want %q and browserDriver subagent", arm.arm, said, arm.want)
		}
		for _, line := range said {
			if !strings.HasPrefix(line, "settings set --scope project ") {
				t.Errorf("arm %s ran %q, which is not a project-scope setting", arm.arm, line)
			}
		}
	}
}
