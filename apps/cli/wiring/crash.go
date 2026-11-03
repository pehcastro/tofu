package wiring

import (
	"tofu/internal/platform/recover"
	"tofu/internal/sys"
)

func CrashTold(project string, tell func(string)) safe.Report {
	return func(p safe.Panic) { tell(p.Saved(sys.CrashDir(project))) }
}
