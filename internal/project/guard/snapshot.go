package guard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const snapshotsKeptPerPath = 20

func snapshot(env Env, ev event) {
	path := ev.ToolInput.FilePath
	if blank(path) {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	rootFull := filepath.Clean(env.Root)
	rel, inside := underRoot(rootFull, path)
	if !inside || blank(rel) || strings.EqualFold(rel, ".boji") || foldPrefix(rel, ".boji/") || ignoredByGit(rootFull, rel) {
		return
	}
	snapshots := filepath.Join(rootFull, ".boji", "snapshots")
	if env.Cache != "" {
		snapshots = filepath.Join(env.Cache, "boji", "snapshots", filepath.Base(rootFull))
	}
	now := time.Now()
	stamp := now.Format("20060102T150405") + fmt.Sprintf("%07d", now.Nanosecond()/100)
	relNative := filepath.FromSlash(rel)
	dest := filepath.Join(snapshots, orDefault(ev.SessionID, "nosession"), stamp, relNative)
	if os.MkdirAll(filepath.Dir(dest), 0o755) != nil || os.WriteFile(dest, data, 0o644) != nil {
		return
	}
	pruneSnapshots(snapshots, relNative)
}

func ignoredByGit(root, rel string) bool {
	err := exec.Command("git", "-C", root, "check-ignore", "-q", "--", rel).Run()
	var exited *exec.ExitError
	return err == nil || !errors.As(err, &exited)
}

func pruneSnapshots(snapshots, rel string) {
	var holders []string
	sessions, _ := os.ReadDir(snapshots)
	for _, session := range sessions {
		stamps, _ := os.ReadDir(filepath.Join(snapshots, session.Name()))
		for _, stamp := range stamps {
			dir := filepath.Join(snapshots, session.Name(), stamp.Name())
			if _, err := os.Stat(filepath.Join(dir, rel)); stamp.IsDir() && err == nil {
				holders = append(holders, dir)
			}
		}
	}
	slices.SortStableFunc(holders, func(a, b string) int { return strings.Compare(filepath.Base(b), filepath.Base(a)) })
	for _, dir := range holders[min(snapshotsKeptPerPath, len(holders)):] {
		_ = os.Remove(filepath.Join(dir, rel))
		if !holdsFile(dir) {
			_ = os.RemoveAll(dir)
		}
	}
}

func holdsFile(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
