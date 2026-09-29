package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/bench/browser/airbnb"
)

const fakeTofuLog = "AIRBNB_BENCH_FAKE_TOFU_LOG"

func TestMain(m *testing.M) {
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
