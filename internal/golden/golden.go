package golden

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func Assert(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := string(stored)
	if want == got {
		return
	}
	if ansi.Strip(want) == ansi.Strip(got) {
		t.Errorf("%s matches the golden file once stripped, so only its styling moved", name)
		return
	}
	t.Errorf("%s does not match the golden file\n--- got ---\n%s\n--- want ---\n%s", name, ansi.Strip(got), ansi.Strip(want))
}
