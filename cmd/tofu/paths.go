package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/cli/page"
	"tofu/internal/cli/render"
	"tofu/internal/store/session"
	"tofu/internal/sys"
)

const (
	partialSuffix  = ".partial"
	repoCopySuffix = ".repo"
)

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
	page := cli.Detect(out, os.Environ())
	partial := target + partialSuffix
	if err := os.RemoveAll(partial); err != nil {
		_ = page.Print(out, page.ErrorLine("the half copy "+page.Path(partial)+" was not removed: "+err.Error(), ""))
		return
	}
	copied, err := copyTreeInto(os.DirFS(source), ".", partial)
	if err == nil {
		err = os.Rename(partial, target)
	}
	if err != nil {
		_ = os.RemoveAll(partial)
		_ = page.Print(out, page.ErrorLine(sys.LegacyStateDirName+" not copied to "+sys.StateDirName+", still read from "+page.Path(source)+": "+err.Error(), ""))
		return
	}
	said := strings.Join([]string{sys.LegacyStateDirName + " copied to " + sys.StateDirName, plural(countSessions(source), "session"),
		plural(copied.Files, "file"), widget.Size(int(copied.Bytes)), sys.LegacyStateDirName + " kept"}, " · ")
	_ = page.Print(out, []string{page.Receipt(cli.Added, said, target)})
}

type treeCopy struct {
	Files   int      `json:"files"`
	Bytes   int64    `json:"bytes"`
	Settled int      `json:"settled,omitempty"`
	Beside  []string `json:"beside,omitempty"`
}

func copyTreeInto(source fs.FS, root, target string) (copied treeCopy, err error) {
	err = fs.WalkDir(source, root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		to := filepath.Join(target, filepath.FromSlash(name))
		if entry.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		body, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		copied.Files, copied.Bytes = copied.Files+1, copied.Bytes+int64(len(body))
		there, err := os.ReadFile(to)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return sys.WriteFile(to, body, 0o644)
		case err != nil || bytes.Equal(there, body):
			return err
		}
		copied.Settled++
		suffix, settled := settleConflict(name, there, body)
		if suffix != "" {
			copied.Beside = append(copied.Beside, name+suffix)
		}
		if settled == nil {
			return nil
		}
		return sys.WriteFile(to+suffix, settled, 0o644)
	})
	return copied, err
}

func settleConflict(name string, there, body []byte) (suffix string, settled []byte) {
	kind, _, _ := strings.Cut(name, "/")
	switch {
	case name == "sessions/HEAD" || kind == "cache":
		return "", nil
	case kind == "log" && path.Ext(name) == ".jsonl":
		return "", mergedLines(there, body)
	}
	return repoCopySuffix, body
}

func mergedLines(there, body []byte) []byte {
	var lines []string
	at := map[string]time.Time{}
	for _, line := range strings.Split(string(there)+"\n"+string(body), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if _, held := at[line]; line == "" || held {
			continue
		}
		var stamped struct{ At time.Time }
		_ = json.Unmarshal([]byte(line), &stamped)
		at[line], lines = stamped.At, append(lines, line)
	}
	slices.SortStableFunc(lines, func(a, b string) int { return at[a].Compare(at[b]) })
	return []byte(strings.Join(lines, "\n") + "\n")
}

func countSessions(state string) int {
	listing, err := session.OpenAt(state).Listing()
	if err != nil {
		return 0
	}
	return len(listing.Sessions) + len(listing.Skipped)
}
