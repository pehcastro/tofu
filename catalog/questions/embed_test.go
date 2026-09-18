package questions_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"boji/catalog/questions"
)

func TestEveryShippedSetIsEmbedded(t *testing.T) {
	disk, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read catalog/questions: %v", err)
	}
	want := map[string]bool{}
	for _, e := range disk {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			want[e.Name()] = true
		}
	}
	if len(want) == 0 {
		t.Fatal("no .yaml files on disk, nothing to prove")
	}
	entries, err := fs.ReadDir(questions.Files(), ".")
	if err != nil {
		t.Fatalf("read the embedded catalog: %v", err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
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
	entries, err := fs.ReadDir(questions.Files(), ".")
	if err != nil {
		t.Fatalf("read the embedded catalog: %v", err)
	}
	for _, e := range entries {
		data, err := fs.ReadFile(questions.Files(), e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if strings.ContainsRune(string(data), rune(0x2014)) {
			t.Errorf("%s carries an em dash", e.Name())
		}
	}
}

func TestNoQuestionFileOnDiskCarriesAnEmDash(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read catalog/questions: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if strings.ContainsRune(string(data), rune(0x2014)) {
			t.Errorf("%s carries an em dash", e.Name())
		}
	}
}
