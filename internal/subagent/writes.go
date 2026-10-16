package subagent

import (
	"slices"
	"sync"
)

type Writes struct {
	mu      sync.Mutex
	first   int
	paths   []string
	running []int
}

func (w *Writes) Wrote(paths []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.running) > 0 {
		w.paths = append(w.paths, paths...)
	}
}

func (w *Writes) Begin() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	mark := w.first + len(w.paths)
	w.running = append(w.running, mark)
	return mark
}

func (w *Writes) End(mark int) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	since := slices.Clone(w.paths[mark-w.first:])
	at := slices.Index(w.running, mark)
	w.running = slices.Delete(w.running, at, at+1)
	oldest := w.first + len(w.paths)
	if len(w.running) > 0 {
		oldest = slices.Min(w.running)
	}
	w.paths, w.first = slices.Clone(w.paths[oldest-w.first:]), oldest
	return since
}
