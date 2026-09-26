package sys

import (
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
	if testing.Testing() && !inside(os.TempDir(), home) {
		return StateDir(filepath.Join(testStateParent(), "home")), nil
	}
	return StateDir(home), nil
}

func testStateParent() string {
	return filepath.Join(os.TempDir(), testStateDirName, strconv.Itoa(os.Getpid()))
}

func SourceRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func InsideSourceTree(path string) bool {
	return inside(SourceRoot(), path)
}

func OwnerHomeStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil || inside(os.TempDir(), home) {
		return ""
	}
	return StateDir(home)
}

func IsOwnerCredential(path string) bool {
	return InsideSourceTree(path) || inside(OwnerHomeStateDir(), path)
}

func inside(root, path string) bool {
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

func ProjectStateDir() (string, error) { return ProjectStateDirAt(".") }

func ProjectStateDirAt(dir string) (string, error) {
	full, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	home, err := HomeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ProjectsDirName, ProjectKey(full)), nil
}

func OwnerProjectStateDir(project string) string {
	home := OwnerHomeStateDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ProjectsDirName, ProjectKey(project))
}

func RecordedStateDir(elem ...string) string {
	owner := OwnerProjectStateDir(SourceRoot())
	if owner == "" {
		return ""
	}
	return filepath.Join(append([]string{owner}, elem...)...)
}

func ProjectKey(path string) string {
	volume := filepath.VolumeName(path)
	path = strings.ToUpper(volume) + path[len(volume):]
	return strings.Map(func(r rune) rune {
		if 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' {
			return r
		}
		return '-'
	}, path)
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
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, "library"), nil
}

func stateChild(name string) (string, error) {
	state, err := ProjectStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, name), nil
}

func CalibrationDir() (string, error) { return stateChild("calibration") }

func LogDir() (string, error) { return stateChild("log") }
