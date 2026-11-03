package sys

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const DeskBinary = "tofu-desktop"

func NewestDesk(root, named string) (string, time.Time, error) {
	candidates := []string{named}
	file := DeskBinary
	if runtime.GOOS == "windows" {
		file += ".exe"
	}
	local := filepath.Join(root, "apps", "desk", "target*", "*")
	if named == "" {
		candidates, _ = filepath.Glob(filepath.Join(local, file))
		if target := os.Getenv("CARGO_TARGET_DIR"); target != "" {
			shared, _ := filepath.Glob(filepath.Join(target, "*", file))
			candidates = append(candidates, shared...)
		}
	}
	var newest string
	var built time.Time
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.ModTime().After(built) {
			newest, built = candidate, info.ModTime()
		}
	}
	switch {
	case newest == "" && named != "":
		return "", built, fmt.Errorf("%s is not a file", named)
	case newest == "":
		return "", built, fmt.Errorf("no %s under %s or $CARGO_TARGET_DIR/*: build the desk from HEAD", file, local)
	}
	absolute, err := filepath.Abs(newest)
	return absolute, built, err
}
