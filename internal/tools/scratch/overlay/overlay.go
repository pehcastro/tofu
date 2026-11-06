package overlay

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/platform/scratch"
)

const (
	overlaysFolder = "overlays"
	filesFolder    = "files"
	treeFolder     = "tree"
	goOverlayFile  = "overlay.json"
)

type Overlay struct {
	Dir     string
	root    string
	files   map[string]string
	hidden  []string
	removed context.CancelFunc
}

func Make(root string, files map[string]string, stale time.Duration, hidden ...string) (Overlay, error) {
	scratchRoot, err := scratch.ScratchRootAt(root)
	if err != nil {
		return Overlay{}, err
	}
	parent := filepath.Join(scratchRoot, overlaysFolder)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Overlay{}, err
	}
	entries, _ := os.ReadDir(parent)
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > stale {
			_ = os.RemoveAll(filepath.Join(parent, entry.Name()))
		}
	}
	dir, err := os.MkdirTemp(parent, "")
	if err != nil {
		return Overlay{}, err
	}
	live, removed := context.WithCancel(context.Background())
	o := Overlay{Dir: dir, root: root, files: files, hidden: hidden, removed: removed}
	go o.keepFresh(live, stale/2)
	if err := o.writeFiles(); err != nil {
		return Overlay{}, errors.Join(err, o.Remove())
	}
	return o, nil
}

func (o Overlay) keepFresh(live context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-live.Done():
			return
		case now := <-ticker.C:
			_ = os.Chtimes(o.Dir, now, now)
		}
	}
}

func (o Overlay) writeFiles() error {
	replace := map[string]string{}
	for rel, body := range o.files {
		copied := filepath.Join(o.Dir, filesFolder, filepath.FromSlash(rel))
		if err := writeFile(copied, []byte(body), 0o644); err != nil {
			return err
		}
		replace[filepath.Join(o.root, filepath.FromSlash(rel))] = copied
	}
	for _, rel := range o.hidden {
		replace[filepath.Join(o.root, filepath.FromSlash(rel))] = ""
	}
	listing, err := json.Marshal(map[string]map[string]string{"Replace": replace})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(o.Dir, goOverlayFile), listing, 0o644)
}

func (o Overlay) GoFlag() string {
	return "-overlay=" + filepath.Join(o.Dir, goOverlayFile)
}

func (o Overlay) Tree() (string, error) {
	tree := filepath.Join(o.Dir, treeFolder)
	overlays := filepath.Dir(o.Dir)
	err := filepath.WalkDir(o.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(o.root, path)
		switch {
		case entry.IsDir() && (path == overlays || rel != "." && slices.Contains([]string{".git", "target"}, entry.Name())):
			return filepath.SkipDir
		case entry.IsDir() || !entry.Type().IsRegular() || slices.Contains(o.hidden, filepath.ToSlash(rel)):
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(tree, rel), body, info.Mode().Perm())
	})
	for rel, body := range o.files {
		if err != nil {
			break
		}
		err = writeFile(filepath.Join(tree, filepath.FromSlash(rel)), []byte(body), 0o644)
	}
	return tree, err
}

func (o Overlay) Local(output, from string) string {
	for _, copied := range []string{filepath.Join(o.Dir, filesFolder), filepath.Join(o.Dir, treeFolder)} {
		forms := []string{copied}
		if rel, err := filepath.Rel(from, copied); err == nil {
			forms = append(forms, rel)
		}
		for _, form := range forms {
			for _, spelled := range []string{form + string(filepath.Separator), filepath.ToSlash(form) + "/"} {
				output = strings.ReplaceAll(output, spelled, "")
			}
		}
	}
	return output
}

func (o Overlay) Remove() error {
	o.removed()
	return os.RemoveAll(o.Dir)
}

func writeFile(path string, body []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, mode)
}
