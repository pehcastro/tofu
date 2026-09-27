package subagent

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/sys"
)

func Assign(parent, name, model string) (string, error) {
	path := filepath.Join(parent, sys.StateDirName, AssignmentFile)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return path, err
	}
	var kept []string
	dropping := false
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		key, _, _ := strings.Cut(line, ":")
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			dropping = strings.TrimSpace(key) == name
		}
		if !dropping && strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, err
	}
	return path, os.WriteFile(path, []byte(strings.Join(append(kept, name+": "+model), "\n")+"\n"), 0o644)
}
