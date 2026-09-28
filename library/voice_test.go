package library_test

import (
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestNoShippedFileSpeaksForAPersonOrADate(t *testing.T) {
	personalVoice := regexp.MustCompile(`(?i)\bthe owner('s\b|\s+(on|said|says|decided|asked|wants|wanted|chose|prefers|preferred|ruled|moved|added)\b)|\bthe user said\b|\b\d{4}-\d{2}-\d{2}\b`)
	checked := 0
	err := fs.WalkDir(os.DirFS("."), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && name == "changelog" {
			return fs.SkipDir
		}
		if entry.IsDir() || (!strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".md")) {
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		checked++
		for i, line := range strings.Split(string(data), "\n") {
			if found := personalVoice.FindString(line); found != "" {
				t.Errorf("library/%s:%d says %q, and shipped text states behaviour rather than who said it or when", name, i+1, found)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the library: %v", err)
	}
	if checked == 0 {
		t.Fatal("no shipped file was read, so this test proves nothing")
	}
}
