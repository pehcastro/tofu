package boardy

import (
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Changed struct {
	Board string   `json:"board"`
	Paths []string `json:"paths"`
}

type fileStamp struct {
	size     int64
	modified time.Time
}

type Watcher struct {
	boards string
	seen   map[string]fileStamp
}

func NewWatcher(boards string) *Watcher {
	return &Watcher{boards: boards, seen: stamps(boards)}
}

func (w *Watcher) Scan() []Changed {
	now := stamps(w.boards)
	byBoard := map[string][]string{}
	note := func(path string) {
		board, _, _ := strings.Cut(path, "/")
		byBoard[board] = append(byBoard[board], path)
	}
	for path, stamp := range now {
		if before, ok := w.seen[path]; !ok || before != stamp {
			note(path)
		}
	}
	for path := range w.seen {
		if _, ok := now[path]; !ok {
			note(path)
		}
	}
	w.seen = now
	changes := make([]Changed, 0, len(byBoard))
	for _, board := range slices.Sorted(maps.Keys(byBoard)) {
		changes = append(changes, Changed{Board: board, Paths: slices.Sorted(slices.Values(byBoard[board]))})
	}
	return changes
}

func (w *Watcher) Watch(quit <-chan struct{}, every time.Duration, send func(Changed)) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-quit:
			return
		case <-tick.C:
			for _, change := range w.Scan() {
				send(change)
			}
		}
	}
}

func stamps(boards string) map[string]fileStamp {
	found := map[string]fileStamp{}
	_ = filepath.WalkDir(boards, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() && entry.Name() == locksDirName {
			return fs.SkipDir
		}
		if err != nil || entry.IsDir() || editorScratch(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		relative, err := filepath.Rel(boards, path)
		if err != nil {
			return nil
		}
		found[filepath.ToSlash(relative)] = fileStamp{size: info.Size(), modified: info.ModTime()}
		return nil
	})
	return found
}

func editorScratch(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~")
}
