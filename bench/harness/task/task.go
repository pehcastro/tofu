package task

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const Dir = "bench/harness/task"

type Task struct {
	Name    string
	Version int
}

func All() []Task {
	return []Task{
		{Name: "hono", Version: 1},
		{Name: "hono", Version: 2},
		{Name: "notes", Version: 3},
		{Name: "exports", Version: 4},
	}
}

func Of(version int) (Task, error) {
	var named []string
	for _, t := range All() {
		if t.Version == version {
			return t, nil
		}
		named = append(named, fmt.Sprintf("%s v%d", t.Name, t.Version))
	}
	return Task{}, fmt.Errorf("v%d names no benched task, so nothing says which tree it starts from: the benched tasks are %s",
		version, strings.Join(named, ", "))
}

type Part string

const (
	Prompt    Part = "prompt.txt"
	Checklist Part = "checklist.md"
	Checker   Part = "check.ts"
	Seed      Part = "seed"
)

func Path(root string, version int, part Part) string {
	return filepath.Join(root, filepath.FromSlash(Dir), fmt.Sprintf("v%d.%s", version, part))
}

func Revision(root string, version int, part Part) (string, error) {
	path := Path(root, version, part)
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("every v%d run is scored against the %s tracked at %s, and it could not be read, so no row from this run can be trusted: %w", version, part, path, err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
