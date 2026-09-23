package report

import (
	"fmt"
	"os"
)

func Write(path string, body []byte, mode os.FileMode, noun string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s is already on disk and a recorded %s is not overwritten, so this run is not stored: move it aside or run on another date", path, noun)
	}
	return os.WriteFile(path, body, mode)
}
