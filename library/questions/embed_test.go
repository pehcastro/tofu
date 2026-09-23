package questions_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"tofu/library/questions"
)

func yamlPaths(t *testing.T, files fs.FS) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(name, ".yaml") {
			return err
		}
		found[name] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walking %v: %v", files, err)
	}
	return found
}

func TestEveryShippedSetIsEmbedded(t *testing.T) {
	want := yamlPaths(t, os.DirFS("."))
	if len(want) == 0 {
		t.Fatal("no .yaml files on disk, nothing to prove")
	}
	got := yamlPaths(t, questions.Files())
	for name := range want {
		if !got[name] {
			t.Errorf("%s is on disk but not embedded", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("%s is embedded but not on disk", name)
		}
	}
}

func TestNoShippedSetCarriesAnEmDash(t *testing.T) {
	for name := range yamlPaths(t, questions.Files()) {
		data, err := fs.ReadFile(questions.Files(), name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.ContainsRune(string(data), rune(0x2014)) {
			t.Errorf("%s carries an em dash", name)
		}
	}
}

func TestNoQuestionFileOnDiskCarriesAnEmDash(t *testing.T) {
	for name := range yamlPaths(t, os.DirFS(".")) {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.ContainsRune(string(data), rune(0x2014)) {
			t.Errorf("%s carries an em dash", name)
		}
	}
}
