package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	chains     = 10
	correction = "never run cargo with more than 2 jobs"
)

func drive(t *testing.T, tofu, home, project, script string) string {
	t.Helper()
	dir := t.TempDir()
	steps, cassette := filepath.Join(dir, "steps.drive"), filepath.Join(dir, "reply.cassette")
	if err := os.WriteFile(steps, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cassette, []byte(`{"text":"done"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(tofu, "drive", steps, "--dir", project, "--home", home, "--cassette", cassette, "--plain", "--fresh").CombinedOutput()
	if err != nil {
		t.Fatalf("tofu drive: %v\n%s", err, out)
	}
	return string(out)
}

func TestRepeatsOfOneCorrectionAcrossTenDrivenChains(t *testing.T) {
	tofu := os.Getenv("TOFU_BIN")
	if tofu == "" {
		t.Skip("TOFU_BIN names the tofu binary this bench drives, and it is not set")
	}
	for _, arm := range []struct {
		name     string
		remember bool
	}{{"memory on: kept with /remember", true}, {"memory off: nothing kept", false}} {
		home, project := t.TempDir(), t.TempDir()
		if arm.remember {
			drive(t, tofu, home, project, "type /remember "+correction+"\nkey enter\nwait remembered for you\n")
		}
		repeats := 0
		for range chains - 1 {
			sent := drive(t, tofu, home, project, "type build the project and run its tests\nkey enter\nwait cooked for\nrequests text 1\n")
			if !strings.Contains(sent, "message 1 system") {
				t.Fatalf("the drive printed no first request:\n%s", sent)
			}
			if !strings.Contains(sent, correction) {
				repeats++
			}
		}
		t.Logf("%-32s the person repeats the correction in %d of the %d chains after the first", arm.name, repeats, chains-1)
	}
}
