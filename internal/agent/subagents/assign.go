package subagent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing/fstest"

	"tofu/internal/sys"
)

func Assign(parent, name, model string) (string, error) {
	path, kept, _, err := assignmentsWithout(parent, name)
	if err != nil {
		return path, err
	}
	return path, writeAssignments(path, append(kept, name+": "+model))
}

func Unassign(parent, name string) (path, was string, err error) {
	path, kept, was, err := assignmentsWithout(parent, name)
	if err != nil || was == "" {
		return path, was, err
	}
	return path, was, writeAssignments(path, kept)
}

func assignmentsWithout(parent, name string) (path string, kept []string, was string, err error) {
	path = filepath.Join(parent, sys.StateDirName, AssignmentFile)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return path, nil, "", err
	}
	dropping := false
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		key, value, _ := strings.Cut(line, ":")
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			dropping = strings.TrimSpace(key) == name
			if dropping {
				was = strings.TrimSpace(value)
			}
		}
		if !dropping && strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	return path, kept, was, nil
}

func writeAssignments(path string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func DefinitionFile(name, description, model string, tools []string) ([]byte, error) {
	header := fmt.Sprintf("---\nname: %s\ndescription: %s\nmodel: %s\n", name, strconv.Quote(description), model)
	if len(tools) > 0 {
		header += "tools: " + strings.Join(tools, ", ") + "\n"
	}
	data := []byte(header + "---\n\n# " + name + "\n")
	parsed, err := read(fstest.MapFS{name: {Data: data}}, name)
	if err != nil {
		return nil, err
	}
	if parsed.Name != name || parsed.Description != description || parsed.Written != model || !slices.Equal(parsed.Tools, tools) {
		return nil, fmt.Errorf("the file for %s does not read back as it was written", name)
	}
	return data, nil
}
