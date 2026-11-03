package dummy

import (
	"path/filepath"

	"tofu/internal/sys"
)

func Path(elem ...string) string {
	return filepath.Join(append([]string{sys.SourceRoot(), "internal", "dummy"}, elem...)...)
}
