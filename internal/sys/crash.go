package sys

import (
	"errors"
	"os"

	"tofu/internal/platform/recover"
)

func Crashes(project string) recover.Report {
	return func(p recover.Panic) { p.Saved(CrashDir(project)) }
}

func CrashDir(project string) string {
	state, err := ProjectStateDirAt(project)
	if err != nil {
		return os.TempDir()
	}
	return state
}

func Crashed(project string, err error) error {
	var caught recover.Panic
	if !errors.As(err, &caught) {
		return err
	}
	return errors.New(caught.Saved(CrashDir(project)))
}
