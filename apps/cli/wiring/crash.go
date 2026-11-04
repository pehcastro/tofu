package wiring

import (
	platformproject "tofu/internal/platform/project"
	"tofu/internal/platform/safe"
)

func CrashTold(project string, tell func(string)) safe.Report {
	return func(p safe.Panic) { tell(p.Saved(platformproject.CrashDir(project))) }
}
