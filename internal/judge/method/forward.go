package method

import (
	"io/fs"

	"tofu/internal/judge/points"
)

const (
	TableFile = points.TableFile
	Unwired   = points.Unwired
)

func Load(shipped fs.FS) (points.Table, error) { return points.Load(shipped) }
