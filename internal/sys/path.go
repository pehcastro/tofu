package sys

import (
	"os"
	"path/filepath"
	"runtime"
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
	return StateDir(home), nil
}

func ProjectStateDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return StateDir(wd), nil
}

func CatalogDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, "catalog"), nil
}

func SessionsDir() (string, error) {
	state, err := ProjectStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "sessions"), nil
}

func CalibrationDir() (string, error) {
	state, err := ProjectStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "calibration"), nil
}
