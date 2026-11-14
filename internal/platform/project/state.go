package project

import (
	"context"
	"path/filepath"

	"tofu/internal/platform/fs"
)

func ProjectStateDir() (string, error) { return ProjectStateDirAt(".") }

func ProjectStateDirAt(dir string) (string, error) {
	places, err := ResolvePlaces(dir)
	return places.State, err
}

func OwnerProjectStateDir(project string) string {
	home := fs.OwnerHomeStateDir()
	if home == "" {
		return ""
	}
	registry, err := registryAt(home)
	if err != nil {
		return ""
	}
	places, err := registry.Places(project)
	if err != nil {
		return ""
	}
	return places.State
}

func RecordedStateDir(elem ...string) string {
	owner := OwnerProjectStateDir(fs.SourceRoot())
	if owner == "" {
		return ""
	}
	return filepath.Join(append([]string{owner}, elem...)...)
}

func CalibrationDir() (string, error) {
	places, err := ResolvePlaces(".")
	if err != nil {
		return "", err
	}
	return places.Calibration(), nil
}

func (p Places) Calibration() string { return filepath.Join(p.State, "calibration") }

func LogDir() (string, error) {
	places, err := ResolvePlaces(".")
	return places.Log, err
}

type Places struct {
	State string
	Log   string
}

func ResolvePlaces(dir string) (Places, error) {
	registry, err := ReadRegistry()
	if err != nil {
		return Places{}, err
	}
	return registry.Places(dir)
}

type runPlacesKey struct{}

func WithRunPlaces(ctx context.Context, places Places) context.Context {
	return context.WithValue(ctx, runPlacesKey{}, places)
}

func RunPlaces(ctx context.Context) (Places, error) {
	if places, found := ctx.Value(runPlacesKey{}).(Places); found {
		return places, nil
	}
	return ResolvePlaces(".")
}
