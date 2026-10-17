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
	boards, user string
	seen         map[string]fileStamp
}

func NewWatcher(boards string) *Watcher {
	user, _ := userHooksDir()
	w := &Watcher{boards: boards, user: user}
	w.seen = w.stamps()
	return w
}

func (w *Watcher) stamps() map[string]fileStamp {
	found := stamps(w.boards, "")
	maps.Copy(found, stamps(w.user, filepath.ToSlash(w.user)+"/"))
	return found
}

func (w *Watcher) Scan() []Changed {
	now := w.stamps()
	var keys []string
	for path := range now {
		if board, _, nested := strings.Cut(path, "/"); nested && boardKey().MatchString(board) && !slices.Contains(keys, board) {
			keys = append(keys, board)
		}
	}
	byBoard := map[string][]string{}
	note := func(path string) {
		board, _, _ := strings.Cut(path, "/")
		if !strings.HasPrefix(path, RulesDirName+"/") && !strings.HasPrefix(path, filepath.ToSlash(w.user)+"/") {
			byBoard[board] = append(byBoard[board], path)
			return
		}
		for _, key := range keys {
			byBoard[key] = append(byBoard[key], path)
		}
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

func stamps(root, prefix string) map[string]fileStamp {
	found := map[string]fileStamp{}
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
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
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		found[prefix+filepath.ToSlash(relative)] = fileStamp{size: info.Size(), modified: info.ModTime()}
		return nil
	})
	return found
}

func editorScratch(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~")
}
