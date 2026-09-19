package task

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const Dir = "bench/harness/task"

type Part string

const (
	Prompt    Part = "prompt.txt"
	Checklist Part = "checklist.md"
	Checker   Part = "check.ts"
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
