package sys

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

type ScratchKind string

const (
	ScratchKindSession  ScratchKind = "session"
	ScratchKindShared   ScratchKind = "shared"
	ScratchKindCache    ScratchKind = "cache"
	ScratchKindLeftover ScratchKind = "leftover"
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
	report := ScratchReport{Root: root}
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
