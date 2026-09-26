package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"tofu/internal/session"
	"tofu/internal/sys"
)

const partialSuffix = ".partial"

func copyLegacyStateDirs(out io.Writer) {
	if wd, err := os.Getwd(); err == nil {
		copyLegacyStateDir(out, wd)
	}
	if home, err := os.UserHomeDir(); err == nil {
		copyLegacyStateDir(out, home)
	}
}

func copyLegacyStateDir(out io.Writer, parent string) {
	target := filepath.Join(parent, sys.StateDirName)
	source := filepath.Join(parent, sys.LegacyStateDirName)
	somethingAtTarget, _ := sys.Exists(target)
	sourceIsADir, _ := sys.IsDir(source)
	if somethingAtTarget || !sourceIsADir {
		return
	}
	partial := target + partialSuffix
	if err := os.RemoveAll(partial); err != nil {
		_, _ = fmt.Fprintf(out, "tofu: a half copy at %s could not be removed: %v\n", partial, err)
		return
	}
	files, _, err := copyTreeInto(os.DirFS(source), ".", partial)
	if err == nil {
		err = os.Rename(partial, target)
	}
	if err != nil {
		_ = os.RemoveAll(partial)
		_, _ = fmt.Fprintf(out, "tofu: copying %s to %s failed: %v\n"+
			"      %s is untouched and is still the directory being read.\n\n", source, target, err, source)
		return
	}
	_, _ = fmt.Fprintf(out, "tofu: the data directory is now %s, and this run found only %s.\n"+
		"      copied %s\n          to %s\n"+
		"      %d sessions, %d files\n"+
		"      %s was read and not touched. it is still there, and deleting it is yours to do.\n\n",
		sys.StateDirName, sys.LegacyStateDirName, source, target, countSessions(source), files, source)
}

func copyTreeInto(source fs.FS, root, target string) (files int, size int64, err error) {
	err = fs.WalkDir(source, root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		path := filepath.Join(target, filepath.FromSlash(name))
		if entry.IsDir() {
			return os.MkdirAll(path, 0o755)
		}
		body, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		if err := copyFileOnce(path, body); err != nil {
			return err
		}
		files, size = files+1, size+int64(len(body))
		return nil
	})
	return files, size, err
}

func copyFileOnce(path string, body []byte) error {
	if there, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(there, body) {
			return fmt.Errorf("%s is already there with other bytes", path)
		}
		return nil
	}
	partial := path + partialSuffix
	if err := os.WriteFile(partial, body, 0o644); err != nil {
		return err
	}
	info, err := os.Stat(partial)
	if err != nil {
		return err
	}
	if info.Size() != int64(len(body)) {
		_ = os.Remove(partial)
		return fmt.Errorf("%s holds %d bytes after the copy, want %d", partial, info.Size(), len(body))
	}
	return os.Rename(partial, path)
}

func countSessions(state string) int {
	listing, err := session.OpenAt(state).Listing()
	if err != nil {
		return 0
	}
	return len(listing.Sessions) + len(listing.Skipped)
}
