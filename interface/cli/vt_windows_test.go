package cli

import (
	"os"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func TestAHandleTheConsoleRefusesGoesPlainUnlessColourWasForced(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = read.Close(), write.Close() })
	cases := []struct {
		name    string
		environ []string
		want    colorprofile.Profile
	}{
		{"a pipe colorprofile takes for a terminal", []string{"TTY_FORCE=1"}, colorprofile.NoTTY},
		{"a pipe with FORCE_COLOR", []string{"FORCE_COLOR=1"}, colorprofile.TrueColor},
		{"a pipe with CLICOLOR_FORCE", []string{"CLICOLOR_FORCE=1"}, colorprofile.TrueColor},
	}
	for _, c := range cases {
		if got := Detect(write, c.environ).Profile; got != c.want {
			t.Errorf("%s: profile %s, want %s", c.name, got, c.want)
		}
	}
}
