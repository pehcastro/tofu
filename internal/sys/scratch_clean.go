package sys

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type ScratchKind string

const (
	ScratchKindSession  ScratchKind = "session"
	ScratchKindShared   ScratchKind = "shared"
	ScratchKindCache    ScratchKind = "cache"
	ScratchKindLeftover ScratchKind = "leftover"
)

const (
	artifactSuffix         = ".bin"
	artifactBackups        = "backups"
	artifactHandleChars    = 32
	artifactScanChunkBytes = 1 << 20
	SessionLockFile        = "lock"
)

type ScratchFolder struct {
	Path      string      `json:"path"`
	Kind      ScratchKind `json:"kind"`
	Session   string      `json:"session,omitempty"`
	SessionID string      `json:"session_id,omitempty"`
	Bytes     int64       `json:"bytes"`
	Touched   time.Time   `json:"touched"`
}

type ScratchReport struct {
	Root    string          `json:"root"`
	Folders []ScratchFolder `json:"folders"`
}

type ScratchSweep struct {
	CleanupDays int
	MaxGB       int
	Session     string
	Keep        string
	Cache       bool
	Leftovers   bool
	Now         time.Time
	Live        func(sessionID string) bool
}

func ReadScratch(project string) (ScratchReport, error) {
	root, err := ScratchRootAt(project)
	if err != nil {
		return ScratchReport{}, err
	}
	report := ScratchReport{Root: root, Folders: []ScratchFolder{}}
	for _, kind := range []ScratchKind{ScratchKindShared, ScratchKindCache} {
		if folder, found := measured(filepath.Join(root, string(kind)), kind); found {
			report.Folders = append(report.Folders, folder)
		}
	}
	sessions, _ := os.ReadDir(filepath.Join(root, ScratchSessions))
	for _, entry := range sessions {
		folder, found := measured(filepath.Join(root, ScratchSessions, entry.Name()), ScratchKindSession)
		if !found {
			continue
		}
		id, _ := os.ReadFile(filepath.Join(folder.Path, scratchSessionFile))
		folder.Session, folder.SessionID = entry.Name(), string(id)
		report.Folders = append(report.Folders, folder)
	}
	leftovers, _ := os.ReadDir(filepath.Join(project, StateDirName, "scratch"))
	for _, entry := range leftovers {
		if folder, found := measured(filepath.Join(project, StateDirName, "scratch", entry.Name()), ScratchKindLeftover); found {
			report.Folders = append(report.Folders, folder)
		}
	}
	return report, nil
}

func measured(path string, kind ScratchKind) (ScratchFolder, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ScratchFolder{}, false
	}
	folder := ScratchFolder{Path: path, Kind: kind, Touched: info.ModTime()}
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			folder.Bytes += info.Size()
			if info.ModTime().After(folder.Touched) {
				folder.Touched = info.ModTime()
			}
		}
		return nil
	})
	return folder, true
}

func (r ScratchReport) Removable(sweep ScratchSweep) []ScratchFolder {
	var removed, kept []ScratchFolder
	var total int64
	for _, folder := range r.Folders {
		total += folder.Bytes
		live := folder.Session == sweep.Keep || folder.SessionID != "" && sweep.Live(folder.SessionID)
		switch {
		case folder.Kind == ScratchKindCache && sweep.Cache, folder.Kind == ScratchKindLeftover && sweep.Leftovers:
			removed = append(removed, folder)
		case folder.Kind != ScratchKindSession || live:
		case sweep.Session != "":
			if folder.Session == sweep.Session || folder.SessionID == sweep.Session {
				removed = append(removed, folder)
			}
		case !folder.Touched.After(sweep.Now.AddDate(0, 0, -sweep.CleanupDays)):
			removed = append(removed, folder)
		default:
			kept = append(kept, folder)
		}
	}
	for _, folder := range removed {
		total -= folder.Bytes
	}
	if sweep.Session != "" {
		return removed
	}
	slices.SortFunc(kept, func(a, b ScratchFolder) int { return a.Touched.Compare(b.Touched) })
	for _, folder := range kept {
		if total <= int64(sweep.MaxGB)<<30 {
			break
		}
		removed, total = append(removed, folder), total-folder.Bytes
	}
	return removed
}

func (p ScratchPlace) Settle(report string) error {
	if !filepath.IsAbs(p.Root) || p.Agent == "" || p.Agent != filepath.Base(p.Agent) || p.Agent == "." || p.Agent == ".." {
		return nil
	}
	dir := p.Dir()
	folded := func(path string) string { return strings.ToLower(filepath.ToSlash(path)) }
	if !strings.Contains(folded(report), folded(filepath.Join(dir, scratchOut))) {
		return os.RemoveAll(dir)
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	failed := []error{err}
	for _, entry := range entries {
		if entry.Name() != scratchOut {
			failed = append(failed, os.RemoveAll(filepath.Join(dir, entry.Name())))
		}
	}
	return errors.Join(failed...)
}

func RemoveAged(dir, prefix, suffix string, before time.Time) error {
	return removeEach(dir, agedFiles(dir, prefix, suffix, before))
}

func RemoveUnreferencedArtifacts(artifacts, sessions string, before time.Time) error {
	candidates := map[string]bool{}
	for _, name := range agedFiles(artifacts, "", artifactSuffix, before) {
		candidates[strings.TrimSuffix(name, artifactSuffix)] = true
	}
	referrers := []string{filepath.Join(artifacts, artifactBackups)}
	sessionDirs, _ := os.ReadDir(sessions)
	for _, entry := range sessionDirs {
		referrers = append(referrers, filepath.Join(sessions, entry.Name()))
	}
	for _, dir := range referrers {
		files, _ := os.ReadDir(dir)
		if len(candidates) == 0 || !activeSince(files, before) {
			continue
		}
		for _, file := range files {
			if file.Type().IsRegular() {
				forgetReferenced(filepath.Join(dir, file.Name()), candidates)
			}
		}
	}
	names := make([]string, 0, len(candidates))
	for id := range candidates {
		names = append(names, id+artifactSuffix)
	}
	return removeEach(artifacts, names)
}

func activeSince(files []fs.DirEntry, since time.Time) bool {
	return slices.ContainsFunc(files, func(file fs.DirEntry) bool {
		info, err := file.Info()
		return file.Name() == SessionLockFile || err == nil && info.ModTime().After(since)
	})
}

func forgetReferenced(path string, candidates map[string]bool) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	chunk, run := make([]byte, artifactScanChunkBytes), make([]byte, 0, artifactHandleChars+1)
	ended := func() {
		if len(run) == artifactHandleChars && candidates[string(run)] {
			delete(candidates, string(run))
		}
		run = run[:0]
	}
	for {
		read, err := file.Read(chunk)
		for _, b := range chunk[:read] {
			if '0' <= b && b <= '9' || 'a' <= b && b <= 'f' {
				if len(run) <= artifactHandleChars {
					run = append(run, b)
				}
				continue
			}
			ended()
		}
		if err != nil {
			ended()
			return
		}
	}
}

func agedFiles(dir, prefix, suffix string, before time.Time) []string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() && strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix) && info.ModTime().Before(before) {
			names = append(names, name)
		}
	}
	return names
}

func removeEach(dir string, names []string) error {
	if !filepath.IsAbs(dir) {
		return nil
	}
	var failed []error
	for _, name := range names {
		failed = append(failed, os.Remove(filepath.Join(dir, name)))
	}
	return errors.Join(failed...)
}

func RemoveScratch(folders []ScratchFolder, stop func(dir string), tries int, wait time.Duration) error {
	var failed []error
	for _, folder := range folders {
		stop(folder.Path)
		var err error
		for range tries {
			if err = os.RemoveAll(folder.Path); err == nil {
				break
			}
			time.Sleep(wait)
		}
		failed = append(failed, err)
	}
	return errors.Join(failed...)
}
