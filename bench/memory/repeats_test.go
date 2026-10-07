package memory

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
	for _, arm := range []struct{ name, answer string }{{"memory on: the offer accepted", "1"}, {"memory off: the offer declined", "2"}} {
		home, project := t.TempDir(), t.TempDir()
		drive(t, tofu, home, project, "type remember: "+correction+"\nkey enter\nwait Remember this?\nkey "+arm.answer+"\nwait cooked for\n")
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
