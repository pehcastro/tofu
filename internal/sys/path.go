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

func HomeConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".boji"), nil
}

func ProjectStateDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, ".boji"), nil
}

func CatalogDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, "catalog"), nil
}

func CalibrationDir() (string, error) {
	state, err := ProjectStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "calibration"), nil
}
