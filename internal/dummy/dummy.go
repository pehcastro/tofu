package dummy

import (
	"path/filepath"

	"tofu/internal/platform/fs"
)

func Path(elem ...string) string {
	return filepath.Join(append([]string{fs.SourceRoot(), "internal", "dummy"}, elem...)...)
}
