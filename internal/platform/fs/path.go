package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func OS() string {
	return runtime.GOOS
}

func Arch() string {
	return runtime.GOARCH
}

func Join(elem ...string) string {
	return filepath.Join(elem...)
}

const (
	StateDirName       = ".tofu"
	LegacyStateDirName = ".boji"
	testStateDirName   = "tofu-test-state"
	ProjectsDirName    = "projects"
	QuotaDirName       = "quota"
)

func StateDir(parent string) string {
	current := filepath.Join(parent, StateDirName)
	if info, err := os.Stat(current); err == nil && info.IsDir() {
		return current
	}
	legacy := filepath.Join(parent, LegacyStateDirName)
	if info, err := os.Stat(legacy); err == nil && info.IsDir() {
		return legacy
	}
	return current
}

func HomeConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if err := usableHome(home); err != nil {
		return "", err
	}
	if testing.Testing() && !Inside(os.TempDir(), home) && !Inside(os.Getenv("GOTMPDIR"), home) {
		return StateDir(filepath.Join(testStateParent(), "home")), nil
	}
	return StateDir(home), nil
}

func usableHome(home string) error {
	if !filepath.IsAbs(home) {
		return fmt.Errorf("home %q is not an absolute path", home)
	}
	for dir := home; ; dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err == nil && info.IsDir() {
			return nil
		}
		if err == nil {
			return fmt.Errorf("home %s is unusable: %s is a file, not a directory", home, dir)
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("home %s is unusable: %w", home, err)
		}
		if filepath.Dir(dir) == dir {
			return fmt.Errorf("home %s is unusable: no part of it exists", home)
		}
	}
}

func testStateParent() string {
	return filepath.Join(os.TempDir(), testStateDirName, strconv.Itoa(os.Getpid()))
}

func SourceRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	for dir := filepath.Dir(file); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

func InsideSourceTree(path string) bool {
	return Inside(SourceRoot(), path)
}

func OwnerHomeStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil || Inside(os.TempDir(), home) {
		return ""
	}
	return StateDir(home)
}

func IsOwnerCredential(path string) bool {
	return InsideSourceTree(path) || Inside(OwnerHomeStateDir(), path)
}

func Inside(root, path string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && !strings.HasPrefix(rel, "..")
}

func ProjectConfigDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if testing.Testing() && InsideSourceTree(wd) {
		return StateDir(testStateParent()), nil
	}
	return StateDir(wd), nil
}

func MovedStateNames() []string {
	return []string{"sessions", "log", "artifacts", "cache", "salvage", "calibration", "shells", "promotions.jsonl", QuotaDirName}
}

func QuotaDir() (string, error) {
	home, err := HomeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, QuotaDirName), nil
}

func LibraryDir() (string, error) {
	project, err := ProjectConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(project, "library"), nil
}
