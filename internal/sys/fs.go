package sys

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

func ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".boji-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	abort := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return abort(err)
	}
	if err := tmp.Close(); err != nil {
		return abort(err)
	}
	if err := os.Chmod(name, perm); err != nil {
		return abort(err)
	}
	if err := os.Rename(name, path); err != nil {
		return abort(err)
	}
	return nil
}

func Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func IsDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

func ListFiles(dir string, suffix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if suffix != "" && filepath.Ext(entry.Name()) != suffix {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func Readable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func WalkDir(dir string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(dir, fn)
}
