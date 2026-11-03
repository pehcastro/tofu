package sweep

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type PackageLines struct {
	Package string
	Source  int
	Test    int
}

func CountLines(benchRoot string) ([]PackageLines, error) {
	entries, err := os.ReadDir(benchRoot)
	if err != nil {
		return nil, err
	}
	var counted []PackageLines
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		source, test, err := countDir(filepath.Join(benchRoot, entry.Name()))
		if err != nil {
			return nil, err
		}
		if source == 0 && test == 0 {
			continue
		}
		counted = append(counted, PackageLines{Package: entry.Name(), Source: source, Test: test})
	}
	sort.Slice(counted, func(i, j int) bool { return counted[i].Package < counted[j].Package })
	return counted, nil
}

func countDir(dir string) (source, test int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		lines, err := countFileLines(filepath.Join(dir, entry.Name()))
		if err != nil {
			return 0, 0, err
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			test += lines
			continue
		}
		source += lines
	}
	return source, test, nil
}

func countFileLines(path string) (int, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(body) == 0 {
		return 0, nil
	}
	lines := bytes.Count(body, []byte("\n"))
	if body[len(body)-1] != '\n' {
		lines++
	}
	return lines, nil
}
