package main

import (
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
	files, err := copyTreeInto(os.DirFS(source), partial)
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

func copyTreeInto(source fs.FS, target string) (int, error) {
	if err := os.RemoveAll(target); err != nil {
		return 0, err
	}
	files := 0
	err := fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		path := filepath.Join(target, filepath.FromSlash(name))
		if entry.IsDir() {
			return os.MkdirAll(path, 0o755)
		}
		reader, err := source.Open(name)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()
		writer, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(writer, reader); err != nil {
			_ = writer.Close()
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		files++
		return nil
	})
	return files, err
}

func countSessions(state string) int {
	listing, err := session.NewStore(filepath.Join(state, "sessions")).Listing()
	if err != nil {
		return 0
	}
	return len(listing.Sessions) + len(listing.Skipped)
}
