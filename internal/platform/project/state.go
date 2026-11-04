package project

import (
	"path/filepath"

	"tofu/internal/platform/fs"
)

func ProjectStateDir() (string, error) { return ProjectStateDirAt(".") }

func ProjectStateDirAt(dir string) (string, error) {
	home, err := fs.HomeConfigDir()
	if err != nil {
		return "", err
	}
	return projectStateIn(home, dir)
}

func OwnerProjectStateDir(project string) string {
	home := fs.OwnerHomeStateDir()
	if home == "" {
		return ""
	}
	state, err := projectStateIn(home, project)
	if err != nil {
		return ""
	}
	return state
}

func RecordedStateDir(elem ...string) string {
	owner := OwnerProjectStateDir(fs.SourceRoot())
	if owner == "" {
		return ""
	}
	return filepath.Join(append([]string{owner}, elem...)...)
}

func stateChild(name string) (string, error) {
	state, err := ProjectStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, name), nil
}

func CalibrationDir() (string, error) { return stateChild("calibration") }

func LogDir() (string, error) {
	places, err := ResolvePlaces(".")
	return places.Log, err
}

type Places struct {
	State string
	Log   string
}

func ResolvePlaces(dir string) (Places, error) {
	home, err := fs.HomeConfigDir()
	if err != nil {
		return Places{}, err
	}
	state, err := projectStateIn(home, dir)
	if err != nil {
		return Places{}, err
	}
	return Places{State: state, Log: filepath.Join(state, "log")}, nil
}
