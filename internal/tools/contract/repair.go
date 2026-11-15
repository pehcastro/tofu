package contract

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/librarian/rules"
	"tofu/internal/platform/outside"
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
	if listed, err := GitListing(context.Background(), r); err == nil {
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
			if listed, listErr = GitListing(context.Background(), r); listErr != nil {
				listed, listErr = WalkListing(r)
			}
		}
		if len(namedLike(r, listed, candidate)) > 0 {
			real = append(real, candidate)
		}
	}
	return real
}

func GitListing(ctx context.Context, r Root) ([]string, error) {
	listed, err := outside.Command(ctx, "git", "-C", string(r), "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, err
	}
	return strings.FieldsFunc(string(listed), func(c rune) bool { return c == 0 }), nil
}

var ErrListingBound = errors.New("the tree holds more files and folders than the listing walks")

type ignoreRule struct {
	base, pattern             string
	negate, dirOnly, anchored bool
}

func ignoreRules(dir, base string) []ignoreRule {
	raw, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	var read []ignoreRule
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := ignoreRule{base: base}
		line, rule.negate = strings.CutPrefix(line, "!")
		line, rule.dirOnly = strings.CutSuffix(line, "/")
		rule.anchored = strings.Contains(line, "/")
		rule.pattern = strings.TrimPrefix(line, "/")
		read = append(read, rule)
	}
	return read
}

func ignored(rules []ignoreRule, rel string, dir bool) bool {
	out := false
	for _, rule := range rules {
		under, inside := strings.CutPrefix(rel, rule.base)
		if !inside || rule.dirOnly && !dir {
			continue
		}
		if !rule.anchored {
			under = path.Base(under)
		}
		if globMatch(strings.Split(rule.pattern, "/"), strings.Split(under, "/")) {
			out = !rule.negate
		}
	}
	return out
}

func globMatch(pattern, name []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			for at := range len(name) + 1 {
				if globMatch(pattern[1:], name[at:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if matched, _ := path.Match(pattern[0], name[0]); !matched {
			return false
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}

func BoundedListing(ctx context.Context, r Root, most int) ([]string, error) {
	var listed []string
	var ignores []ignoreRule
	walked := 0
	err := filepath.WalkDir(string(r), func(full string, entry fs.DirEntry, err error) error {
		rel, relErr := filepath.Rel(string(r), full)
		slash := filepath.ToSlash(rel)
		switch {
		case err != nil || relErr != nil:
			return cmp.Or(err, relErr)
		case ctx.Err() != nil:
			return ctx.Err()
		case walked == most:
			return ErrListingBound
		case slash == ".":
			ignores = ignoreRules(full, "")
			return nil
		case entry.IsDir() && (neverListed(entry.Name()) || ignored(ignores, slash, true)):
			return fs.SkipDir
		case ignored(ignores, slash, entry.IsDir()):
			return nil
		}
		walked++
		if entry.IsDir() {
			ignores = append(ignores, ignoreRules(full, slash+"/")...)
			return nil
		}
		listed = append(listed, slash)
		return nil
	})
	return listed, err
}

func neverListed(name string) bool {
	return rules.NeverWalked(name) || slices.Contains([]string{".local", "target", "zig-out", ".zig-cache", "zig-cache", "zig-pkg"}, name)
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
