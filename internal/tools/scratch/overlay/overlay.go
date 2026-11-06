package overlay

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
	Dir   string
	root  string
	files map[string]string
}

func Make(root string, files map[string]string, stale time.Duration) (Overlay, error) {
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
	o := Overlay{Dir: dir, root: root, files: files}
	if err := o.writeFiles(); err != nil {
		return Overlay{}, errors.Join(err, o.Remove())
	}
	return o, nil
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
		case entry.IsDir() || !entry.Type().IsRegular():
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
		err = os.WriteFile(filepath.Join(tree, filepath.FromSlash(rel)), []byte(body), 0o644)
	}
	return tree, err
}

func (o Overlay) Remove() error {
	return os.RemoveAll(o.Dir)
}

func writeFile(path string, body []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, mode)
}
