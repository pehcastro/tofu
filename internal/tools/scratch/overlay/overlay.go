package overlay

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	iofs "io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"tofu/internal/platform/fs"
	"tofu/internal/platform/outside"
	"tofu/internal/platform/scratch"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/glob"
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

func (o Overlay) Tree(ctx context.Context) (string, error) {
	tree := filepath.Join(o.Dir, treeFolder)
	listed, err := o.readable(ctx)
	copiers := runtime.GOMAXPROCS(0)
	failed := make([]error, copiers)
	var copying sync.WaitGroup
	for at := range copiers {
		copying.Go(func() {
			for i := at; i < len(listed) && failed[at] == nil; i += copiers {
				rel := filepath.FromSlash(listed[i])
				failed[at] = cmp.Or(ctx.Err(), copyRegular(filepath.Join(o.root, rel), filepath.Join(tree, rel)))
			}
		})
	}
	copying.Wait()
	err = cmp.Or(err, ctx.Err(), errors.Join(failed...))
	for rel, body := range o.files {
		if err != nil {
			break
		}
		err = writeFile(filepath.Join(tree, filepath.FromSlash(rel)), []byte(body), 0o644)
	}
	return tree, err
}

func (o Overlay) readable(ctx context.Context) ([]string, error) {
	var listed []string
	tracked, err := outside.Command(ctx, "git", "-C", o.root, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	switch {
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case err == nil:
		listed = strings.FieldsFunc(string(tracked), func(c rune) bool { return c == 0 })
	default:
		walked, err := glob.FilesUnder(contract.Root(o.root), ".", false)
		if err != nil {
			return nil, err
		}
		listed = walked.Files
	}
	overlays := filepath.Dir(o.Dir)
	return slices.DeleteFunc(listed, func(rel string) bool {
		neverRead := slices.ContainsFunc(strings.Split(rel, "/"), func(part string) bool {
			return slices.Contains([]string{".git", ".local", "target", "zig-out", ".zig-cache", "zig-cache", "zig-pkg", "node_modules"}, part)
		})
		return neverRead || slices.Contains(o.hidden, rel) || fs.Inside(overlays, filepath.Join(o.root, filepath.FromSlash(rel)))
	}), nil
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

func copyRegular(from, to string) error {
	info, err := os.Lstat(from)
	switch {
	case errors.Is(err, iofs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case !info.Mode().IsRegular():
		return nil
	}
	body, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return writeFile(to, body, info.Mode().Perm())
}

func writeFile(path string, body []byte, mode iofs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, mode)
}
