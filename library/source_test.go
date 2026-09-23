package library_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

type citation struct {
	file  string
	line  int
	field string
	path  string
}

func citationsIn(name string, data []byte) []citation {
	var found []citation
	field := ""
	for i, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(raw)
		if item, isItem := strings.CutPrefix(trimmed, "- "); isItem {
			if field == "source" || field == "also" {
				found = append(found, citation{name, i + 1, field, strings.TrimSpace(item)})
			}
			continue
		}
		key, value, split := strings.Cut(trimmed, ":")
		if !split || strings.ContainsAny(key, " \t`*#|-") {
			field = ""
			continue
		}
		field = key
		if (key == "source" || key == "also") && strings.TrimSpace(value) != "" {
			found = append(found, citation{name, i + 1, key, strings.TrimSpace(value)})
		}
	}
	return found
}

func TestNoShippedFileCitesASourceOutsideTheLibrary(t *testing.T) {
	checked := 0
	err := fs.WalkDir(os.DirFS("."), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || (!strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".md")) {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		for _, one := range citationsIn(name, data) {
			checked++
			inside, named := strings.CutPrefix(one.path, "library/")
			if !named {
				t.Errorf("library/%s:%d: %s = %q, and a citation names a path under library/", one.file, one.line, one.field, one.path)
				continue
			}
			if _, err := os.Stat(inside); err != nil {
				t.Errorf("library/%s:%d: %s = %q, which does not resolve: %v", one.file, one.line, one.field, one.path, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the library: %v", err)
	}
	if checked == 0 {
		t.Fatal("no shipped file carries a source, so this test proves nothing")
	}
}
