package sys

import (
	"os"

	"tofu/internal/platform/recover"
)

func Crashes(project string) safe.Report {
	return func(p safe.Panic) {
		state, err := ProjectStateDirAt(project)
		if err != nil {
			state = os.TempDir()
		}
		p.Saved(state)
	}
}
