package shadow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	AbsentMode = "000000"
	AbsentBlob = "0000000000000000000000000000000000000000"
)

type Change struct {
	Path, FromMode, ToMode, FromBlob, ToBlob string
	By                                       []string
}

type Recorded struct {
	Turn
	Start, End string
	EndedAt    time.Time
	Changes    []Change
}

func (r Recorded) Ended() bool { return r.End != "" }

func (s Store) Recorded(ctx context.Context, newest int) ([]Recorded, error) {
	if _, err := os.Stat(filepath.Join(s.gitDir, "HEAD")); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	listed, err := s.git(ctx, "", "for-each-ref", "--sort=-refname", "--sort=-committerdate", "--count="+strconv.Itoa(newest), "--format=%00%(objectname) %(parent) %(committerdate:unix)%0a%(contents)", TurnRefs)
	if err != nil {
		return nil, err
	}
	var turns []Recorded
	var ends strings.Builder
	named := map[string][]File{}
	records := strings.Split(listed, "\x00")[1:]
	slices.Reverse(records)
	for _, record := range records {
		head, body, _ := strings.Cut(record, "\n")
		fields := strings.Fields(head)
		turn, files := parseMessage(body)
		one := Recorded{Turn: turn, Start: fields[0]}
		if len(fields) == 3 {
			seconds, _ := strconv.ParseInt(fields[2], 10, 64)
			one = Recorded{Turn: turn, Start: fields[1], End: fields[0], EndedAt: time.Unix(seconds, 0)}
			named[one.End] = files
			ends.WriteString(one.End + "\n")
		}
		turns = append(turns, one)
	}
	if ends.Len() == 0 {
		return turns, nil
	}
	diffs, err := s.git(ctx, ends.String(), "diff-tree", "--stdin", "-r", "-z", "--no-renames")
	if err != nil {
		return nil, err
	}
	byEnd := map[string][]Change{}
	commit := ""
	fields := strings.Split(strings.TrimSuffix(diffs, "\x00"), "\x00")
	for i := 0; i < len(fields); i++ {
		if !strings.HasPrefix(fields[i], ":") {
			commit = fields[i]
			continue
		}
		if i+1 == len(fields) {
			return nil, s.badRaw(fields[i])
		}
		change, err := s.rawChange(fields[i], fields[i+1])
		if err != nil {
			return nil, err
		}
		i++
		if at := slices.IndexFunc(named[commit], func(file File) bool { return file.Path == change.Path }); at >= 0 {
			change.By = named[commit][at].By
		}
		byEnd[commit] = append(byEnd[commit], change)
	}
	for i := range turns {
		turns[i].Changes = byEnd[turns[i].End]
	}
	return turns, nil
}

func (s Store) Changed(ctx context.Context, from, to string) ([]Change, error) {
	out, err := s.Git(ctx, "", "diff-tree", "-r", "-z", "--no-renames", from, to)
	if err != nil || out == "" {
		return nil, err
	}
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var changes []Change
	for i := 0; i+1 < len(fields); i += 2 {
		change, err := s.rawChange(fields[i], fields[i+1])
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func (s Store) rawChange(meta, path string) (Change, error) {
	modes := strings.Fields(strings.TrimPrefix(meta, ":"))
	if len(modes) != 5 {
		return Change{}, s.badRaw(meta)
	}
	return Change{Path: path, FromMode: modes[0], ToMode: modes[1], FromBlob: modes[2], ToBlob: modes[3]}, nil
}

func (s Store) badRaw(field string) error {
	return fmt.Errorf("git diff-tree in the shadow store %s printed %q where a mode, blob and status line belongs", s.gitDir, field)
}

func (s Store) Restore(ctx context.Context, change Change) error {
	target := filepath.Join(s.work, filepath.FromSlash(change.Path))
	if change.FromMode == AbsentMode {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	body, err := s.Git(ctx, "", "cat-file", "blob", change.FromBlob)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	switch change.FromMode {
	case "100644", "100755":
		perm := os.FileMode(0o644)
		if change.FromMode == "100755" {
			perm = 0o755
		}
		if err := os.WriteFile(target, []byte(body), perm); err != nil {
			return err
		}
		return os.Chmod(target, perm)
	case "120000":
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		return os.Symlink(body, target)
	}
	return fmt.Errorf("the shadow store holds %s with git mode %s, which it does not write back", change.Path, change.FromMode)
}
