package sys

import (
	"errors"
	"os"

	"tofu/internal/platform/safe"
)

func Crashes(project string) safe.Report {
	return func(p safe.Panic) { p.Saved(CrashDir(project)) }
}

func CrashDir(project string) string {
	state, err := ProjectStateDirAt(project)
	if err != nil {
		return os.TempDir()
	}
	return state
}

func Crashed(project string, err error) error {
	var caught safe.Panic
	if !errors.As(err, &caught) {
		return err
	}
	return errors.New(caught.Saved(CrashDir(project)))
}
