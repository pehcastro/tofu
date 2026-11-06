package contract

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/librarian/rules"
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

func Lookalikes(r Root, requested string) ([]string, error) {
	if listed, err := GitListing(r); err == nil {
		if found := namedLike(r, listed, requested); len(found) > 0 {
			return found, nil
		}
	}
	walked, err := WalkListing(r)
	if err != nil {
		return nil, err
	}
	return namedLike(r, walked, requested), nil
}

func RealPaths(r Root, candidates []string) []string {
	var real, listed []string
	var listErr error
	for _, candidate := range candidates {
		if resolved, err := r.Resolve(candidate); err == nil {
			if _, err := os.Stat(resolved); err == nil {
				real = append(real, candidate)
				continue
			}
		}
		if listed == nil && listErr == nil {
			if listed, listErr = GitListing(r); listErr != nil {
				listed, listErr = WalkListing(r)
			}
		}
		if len(namedLike(r, listed, candidate)) > 0 {
			real = append(real, candidate)
		}
	}
	return real
}

func GitListing(r Root) ([]string, error) {
	listed, err := exec.Command("git", "-C", string(r), "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, err
	}
	return strings.FieldsFunc(string(listed), func(c rune) bool { return c == 0 }), nil
}

func WalkListing(r Root) ([]string, error) {
	var listed []string
	err := filepath.WalkDir(string(r), func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rules.NeverWalked(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(string(r), full)
		listed = append(listed, filepath.ToSlash(rel))
		return err
	})
	return listed, err
}

func namedLike(r Root, listed []string, requested string) []string {
	wanted := strings.ToLower(path.Clean(filepath.ToSlash(requested)))
	base := path.Base(wanted)
	var found []string
	for _, slash := range listed {
		lower := strings.ToLower(slash)
		named := lower == wanted || path.Base(lower) == base
		if !named || slices.ContainsFunc(strings.Split(path.Dir(slash), "/"), rules.NeverWalked) {
			continue
		}
		if _, err := os.Lstat(filepath.Join(string(r), slash)); err == nil {
			found = append(found, slash)
		}
	}
	slices.Sort(found)
	return slices.Compact(found)
}
