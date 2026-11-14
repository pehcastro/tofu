package shadow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"tofu/internal/konst"
	"tofu/internal/store/snapshot"
)

const TurnRefs = "refs/tofu/turns"

type Store struct{ snapshot.Files }

func At(state, work string) Store {
	if abs, err := filepath.Abs(work); err == nil {
		work = abs
	}
	return Store{snapshot.Files{State: state, Tree: work}}
}

func (s Store) Work() string { return s.Tree }

type Turn struct {
	ID        string
	Session   string
	Agent     string
	SpawnedBy string
	Owns      []string
}

type Call struct {
	ID   string
	Args json.RawMessage
}

type File struct {
	Path    string
	Added   int
	Removed int
	Binary  bool
	By      []string
}

type Changed struct {
	Turn  Turn
	Ended bool
	Files []File
}

type Started struct {
	Store  Store
	Turn   Turn
	Calls  func() []Call
	begun  *begun
	parent *Started
}

type begun struct {
	done   chan struct{}
	tree   string
	commit string
	err    error
}

type startedKey struct{}

func (s Started) Into(ctx context.Context) context.Context {
	if parent, inTurn := ctx.Value(startedKey{}).(Started); inTurn {
		s.parent = &parent
	}
	return context.WithValue(ctx, startedKey{}, s)
}

func (s Started) Parent() (Started, bool) {
	if s.parent == nil || s.parent.Begun() != nil {
		return Started{}, false
	}
	parent := *s.parent
	parent.Calls = func() []Call { return nil }
	return parent, true
}

func StartedIn(ctx context.Context) (Started, bool) {
	started, ok := ctx.Value(startedKey{}).(Started)
	return started, ok && started.Begun() == nil
}

func (s Store) Begin(ctx context.Context, turn Turn, calls func() []Call) Started {
	started := Started{Store: s, Turn: turn, Calls: calls, begun: &begun{done: make(chan struct{})}}
	go func() {
		defer close(started.begun.done)
		timed, cancel := context.WithTimeout(context.WithoutCancel(ctx), konst.ShadowSnapshotTimeoutMillis*time.Millisecond)
		defer cancel()
		tree, err := s.Snapshot(timed)
		if err != nil {
			started.begun.err = err
			return
		}
		commit, err := s.Commit(timed, message(turn, nil), tree)
		if err == nil {
			_, err = s.git(timed, "", "update-ref", TurnRefs+"/"+turn.ID, commit)
		}
		started.begun.tree, started.begun.commit, started.begun.err = tree, commit, err
	}()
	return started
}

func (s Started) Begun() error {
	<-s.begun.done
	return s.begun.err
}

func (s Started) StartTree() (string, error) {
	err := s.Begun()
	return s.begun.tree, err
}

func (s Started) End(ctx context.Context) ([]string, error) {
	if err := s.Begun(); err != nil {
		return nil, err
	}
	tree, err := s.Store.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	files, err := s.Store.diff(ctx, s.begun.commit, tree, s.Calls())
	if err != nil {
		return nil, err
	}
	commit, err := s.Store.Commit(ctx, message(s.Turn, files), tree, s.begun.commit)
	if err != nil {
		return nil, err
	}
	if _, err := s.Store.git(ctx, "", "update-ref", TurnRefs+"/"+s.Turn.ID, commit, s.begun.commit); err != nil {
		return nil, err
	}
	return s.Store.prune(ctx)
}

func (s Started) Live(ctx context.Context) (Changed, error) {
	if err := s.Begun(); err != nil {
		return Changed{}, err
	}
	tree, err := s.Store.Snapshot(ctx)
	if err != nil {
		return Changed{}, err
	}
	files, err := s.Store.diff(ctx, s.begun.commit, tree, s.Calls())
	return Changed{Turn: s.Turn, Files: files}, err
}

func (s Store) Turns(ctx context.Context, count int) ([]Changed, error) {
	recorded, err := s.Recorded(ctx, count)
	if err != nil {
		return nil, err
	}
	var turns []Changed
	for _, one := range slices.Backward(recorded) {
		changed := Changed{Turn: one.Turn, Ended: one.Ended()}
		if one.Ended() {
			if changed.Files, err = s.diff(ctx, one.Start, one.End, nil); err != nil {
				return nil, err
			}
			for i, file := range changed.Files {
				if at := slices.IndexFunc(one.Changes, func(c Change) bool { return c.Path == file.Path }); at >= 0 {
					changed.Files[i].By = one.Changes[at].By
				}
			}
		}
		turns = append(turns, changed)
	}
	return turns, nil
}

func (s Store) diff(ctx context.Context, from, to string, calls []Call) ([]File, error) {
	out, err := s.git(ctx, "", "diff-tree", "-r", "-z", "--numstat", "--no-renames", from, to)
	if err != nil {
		return nil, err
	}
	var files []File
	for _, field := range strings.Split(strings.TrimSuffix(out, "\x00"), "\x00") {
		counts := strings.SplitN(field, "\t", 3)
		if len(counts) != 3 {
			continue
		}
		added, addedErr := strconv.Atoi(counts[0])
		removed, removedErr := strconv.Atoi(counts[1])
		files = append(files, File{Path: counts[2], Added: added, Removed: removed, Binary: addedErr != nil || removedErr != nil, By: s.namedBy(counts[2], calls)})
	}
	return files, nil
}

func (s Store) namedBy(path string, calls []Call) []string {
	var by []string
	work := filepath.ToSlash(s.Work()) + "/"
	for _, call := range calls {
		var args any
		if json.Unmarshal(call.Args, &args) != nil || slices.Contains(by, call.ID) {
			continue
		}
		if slices.ContainsFunc(stringsIn(args), func(said string) bool {
			said = strings.ReplaceAll(filepath.ToSlash(said), work, "")
			return mentions(strings.ReplaceAll(" "+said, " ./", " "), path)
		}) {
			by = append(by, call.ID)
		}
	}
	return by
}

func stringsIn(value any) []string {
	switch value := value.(type) {
	case string:
		return []string{value}
	case []any:
		var found []string
		for _, item := range value {
			found = append(found, stringsIn(item)...)
		}
		return found
	case map[string]any:
		return stringsIn(slices.Collect(maps.Values(value)))
	}
	return nil
}

func mentions(said, path string) bool {
	pathRune := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-/\\", r)
	}
	for from := 0; ; {
		at := strings.Index(said[from:], path)
		if at < 0 {
			return false
		}
		start, end := from+at, from+at+len(path)
		before, after := ' ', ' '
		if start > 0 {
			before = rune(said[start-1])
		}
		if end < len(said) {
			after = rune(said[end])
		}
		if !pathRune(before) && !pathRune(after) {
			return true
		}
		from = start + 1
	}
}

func (s Store) prune(ctx context.Context) ([]string, error) {
	var listed, counted string
	var listing, counting error
	var asking sync.WaitGroup
	asking.Go(func() {
		listed, listing = s.git(ctx, "", "for-each-ref", "--sort=refname", "--sort=committerdate", "--format=%(refname)", TurnRefs)
	})
	asking.Go(func() { counted, counting = s.git(ctx, "", "count-objects", "-v") })
	asking.Wait()
	if err := errors.Join(listing, counting); err != nil {
		return nil, err
	}
	refs := strings.Fields(listed)
	dropped, notes := max(len(refs)-konst.ShadowTurnsKept, 0), []string{}
	if dropped > 0 {
		notes = append(notes, fmt.Sprintf("the shadow store keeps its last %d turns, so it dropped the %d oldest", konst.ShadowTurnsKept, dropped))
	}
	held := 0
	for line := range strings.Lines(counted) {
		name, kib, _ := strings.Cut(strings.TrimSpace(line), ": ")
		if name == "size" || name == "size-pack" || name == "size-garbage" {
			n, _ := strconv.Atoi(kib)
			held += n * 1024
		}
	}
	packing := held > konst.ShadowStoreKeptBytes
	if packing {
		dropped = min(max(dropped, len(refs)/2, 1), len(refs)-1)
		notes = append(notes, fmt.Sprintf("the shadow store held %d bytes against its cap of %d, so it keeps its newest %d turns and packed them", held, konst.ShadowStoreKeptBytes, len(refs)-dropped))
	}
	if dropped > 0 {
		var deletes strings.Builder
		for _, ref := range refs[:dropped] {
			deletes.WriteString("delete " + ref + "\n")
		}
		if _, err := s.git(ctx, deletes.String(), "update-ref", "--stdin"); err != nil {
			return notes, err
		}
	}
	if !packing {
		return notes, nil
	}
	_, err := s.git(ctx, "", "gc", "--quiet", "--prune="+snapshot.PruneGrace)
	return notes, err
}

func (s Store) git(ctx context.Context, stdin string, args ...string) (string, error) {
	out, err := s.Git(ctx, stdin, args...)
	return strings.TrimSpace(out), err
}

func message(turn Turn, files []File) string {
	lines := []string{"tofu turn " + turn.ID, "", "session " + turn.Session, "agent " + turn.Agent}
	if turn.SpawnedBy != "" {
		lines = append(lines, "spawned-by "+turn.SpawnedBy)
	}
	for _, glob := range turn.Owns {
		lines = append(lines, "owns "+glob)
	}
	for _, file := range files {
		for _, call := range file.By {
			lines = append(lines, "call "+call+" "+file.Path)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func parseMessage(body string) (Turn, []File) {
	var turn Turn
	var files []File
	for line := range strings.Lines(body) {
		key, value, _ := strings.Cut(strings.TrimRight(line, "\r\n"), " ")
		switch key {
		case "tofu":
			turn.ID = strings.TrimPrefix(value, "turn ")
		case "session":
			turn.Session = value
		case "agent":
			turn.Agent = value
		case "spawned-by":
			turn.SpawnedBy = value
		case "owns":
			turn.Owns = append(turn.Owns, value)
		case "call":
			call, path, _ := strings.Cut(value, " ")
			if at := slices.IndexFunc(files, func(one File) bool { return one.Path == path }); at >= 0 {
				files[at].By = append(files[at].By, call)
			} else {
				files = append(files, File{Path: path, By: []string{call}})
			}
		}
	}
	return turn, files
}
