package turn

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/sys"
)

func RepairPath(requested, noun string, found []string) (string, string, error) {
	base := path.Base(path.Clean(filepath.ToSlash(requested)))
	switch len(found) {
	case 0:
		return "", "", fmt.Errorf("%s is not a %s under the working directory, and nothing there is named %s: nothing was run",
			requested, noun, base)
	case 1:
		return found[0], fmt.Sprintf("repaired: the path was %s, which does not exist, and %s is the only %s under the working directory named %s, so that is the one that ran. send that path next time",
			requested, found[0], noun, base), nil
	}
	return "", "", fmt.Errorf("%s is not a %s under the working directory, and %d of them are named %s: %s. name the one you mean, because a repair is only made when it is the only candidate: nothing was run",
		requested, noun, len(found), base, strings.Join(found, ", "))
}

func (r Root) lookalikes(requested string) ([]string, error) {
	wanted := strings.ToLower(path.Clean(filepath.ToSlash(requested)))
	base := path.Base(wanted)
	var found []string
	walkErr := filepath.WalkDir(string(r), func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "node_modules", ".git", sys.StateDirName, sys.LegacyStateDirName:
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(string(r), full)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(rel)
		if lower := strings.ToLower(slash); lower == wanted || path.Base(lower) == base {
			found = append(found, slash)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	slices.Sort(found)
	return slices.Compact(found), nil
}
