package contract

import (
	"cmp"
	"os"
	"path/filepath"
	"strings"
)

func FindUp(dir string, name ...string) (string, bool) {
	at := Absolute(dir)
	for {
		if path := filepath.Join(append([]string{at}, name...)...); IsFile(path) {
			return path, true
		}
		parent := filepath.Dir(at)
		if parent == at {
			return "", false
		}
		at = parent
	}
}

func IsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func Absolute(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

func Listed(ids []string) string {
	return cmp.Or(strings.Join(ids, ", "), "none")
}

func Failure(content string) Result {
	first, _, _ := strings.Cut(content, "\n")
	return Result{Content: content, FailureText: first}
}
