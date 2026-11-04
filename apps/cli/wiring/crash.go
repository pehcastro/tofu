package wiring

import (
	"tofu/internal/platform/recover"
	"tofu/internal/sys"
)

func CrashTold(project string, tell func(string)) recover.Report {
	return func(p recover.Panic) { tell(p.Saved(sys.CrashDir(project))) }
}
