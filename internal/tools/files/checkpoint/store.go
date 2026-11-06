package checkpoint

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/tools/history/shadow"
)

const submodule = "160000"

type repo struct{ shadow.Store }

type turnRef struct {
	id, session, commit, parent string
}

type change struct {
	path, fromMode, toMode, fromBlob, toBlob string
}

func (r repo) turns(ctx context.Context) ([]turnRef, error) {
	listed, err := r.Git(ctx, "", "for-each-ref", "--sort=refname", "--sort=committerdate", "--format=%00%(objectname) %(parent)%0a%(contents)", shadow.TurnRefs)
	if err != nil {
		return nil, err
	}
	var turns []turnRef
	for _, record := range strings.Split(listed, "\x00")[1:] {
		head, body, _ := strings.Cut(record, "\n")
		turn := turnRef{}
		turn.commit, turn.parent, _ = strings.Cut(head, " ")
		for line := range strings.Lines(body) {
			key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
			switch key {
			case "tofu":
				turn.id = strings.TrimPrefix(value, "turn ")
			case "session":
				turn.session = value
			}
		}
		turns = append(turns, turn)
	}
	return turns, nil
}

func (r repo) changed(ctx context.Context, from, to string) ([]change, error) {
	out, err := r.Git(ctx, "", "diff-tree", "-r", "-z", "--no-renames", from, to)
	if err != nil || out == "" {
		return nil, err
	}
	fields := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var changes []change
	for i := 0; i+1 < len(fields); i += 2 {
		meta := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(meta) != 5 {
			return nil, fmt.Errorf("git diff-tree in the shadow store %s printed %q where a mode, blob and status line belongs", r.Dir(), fields[i])
		}
		changes = append(changes, change{path: fields[i+1], fromMode: meta[0], toMode: meta[1], fromBlob: meta[2], toBlob: meta[3]})
	}
	return changes, nil
}

func (r repo) restore(ctx context.Context, file change) error {
	target := filepath.Join(r.Work(), filepath.FromSlash(file.path))
	if file.fromMode == "000000" {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	body, err := r.Git(ctx, "", "cat-file", "blob", file.fromBlob)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	switch file.fromMode {
	case "100644", "100755":
		perm := os.FileMode(0o644)
		if file.fromMode == "100755" {
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
	return fmt.Errorf("the checkpoint holds %s with git mode %s, which checkpoint does not write", file.path, file.fromMode)
}
