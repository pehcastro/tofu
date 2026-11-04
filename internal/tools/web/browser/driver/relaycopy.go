package driver

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"tofu/internal/sys"
)

func CopyPath(home string) string {
	name := "tofu-browser"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(home, sys.StateDirName, "bin", name)
}

func sourceRecord(home string) string {
	return filepath.Join(home, sys.StateDirName, "browser", "tofu-source")
}

func Refresh(home string) (refreshed bool, err error) {
	source, err := os.ReadFile(sourceRecord(home))
	if err != nil {
		return false, ignoreMissing(err)
	}
	from, err := os.Stat(string(source))
	if err != nil {
		return false, err
	}
	browserCopy := CopyPath(home)
	if to, err := os.Stat(browserCopy); err == nil && to.Size() == from.Size() && to.ModTime().Equal(from.ModTime()) {
		return false, nil
	}
	binary, err := os.ReadFile(string(source))
	if err == nil {
		_, err = ReplaceExecutable(browserCopy, binary)
	}
	if err == nil {
		err = os.Chtimes(browserCopy, from.ModTime(), from.ModTime())
	}
	return err == nil, err
}

func RefreshIfCopyOf(home, tofu string) (refreshed bool, err error) {
	source, _ := os.ReadFile(sourceRecord(home))
	from, fromErr := os.Stat(string(source))
	replaced, replacedErr := os.Stat(tofu)
	if fromErr != nil || replacedErr != nil || !os.SameFile(from, replaced) {
		return false, nil
	}
	return Refresh(home)
}

func ReplaceExecutable(target string, binary []byte) (aside string, err error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	staged, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".new-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(staged.Name()) }()
	_, err = staged.Write(binary)
	if err = errors.Join(err, staged.Close(), os.Chmod(staged.Name(), 0o755)); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		return "", os.Rename(staged.Name(), target)
	}
	sweepAsides(target)
	aside = asideOf(target)
	if err := os.Rename(target, aside); errors.Is(err, fs.ErrNotExist) {
		aside = ""
	} else if err != nil {
		return "", err
	}
	if err := os.Rename(staged.Name(), target); err != nil {
		if aside != "" {
			_ = os.Rename(aside, target)
		}
		return "", err
	}
	return aside, nil
}

func asideOf(target string) string {
	return target + ".old-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func sweepAsides(target string) {
	setAside, _ := filepath.Glob(target + ".old*")
	for _, old := range setAside {
		_ = os.Remove(old)
	}
}

func ignoreMissing(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
