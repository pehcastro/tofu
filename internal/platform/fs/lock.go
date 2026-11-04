package fs

import "os"

func TryLock(path string) (*os.File, bool, error) {
	file, held, err := openLocked(path)
	if file == nil {
		return nil, held, err
	}
	opened, err := file.Stat()
	named, namedErr := os.Stat(path)
	if err != nil || namedErr != nil || !os.SameFile(opened, named) {
		_ = file.Close()
		return nil, true, nil
	}
	return file, false, nil
}
