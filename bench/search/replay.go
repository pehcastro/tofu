package search

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	tool "tofu/internal/search"
)

var ErrPathGone = errors.New("bench/search: the recorded path is not in the tree today")

type Outcome struct {
	Row      Recorded
	Answered tool.Candidate
	Attempts []tool.Attempt
}

func (o Outcome) Attempt(candidate tool.Candidate) tool.Attempt {
	for _, attempt := range o.Attempts {
		if attempt.Candidate == candidate {
			return attempt
		}
	}
	return tool.Attempt{Candidate: candidate, NotRun: "the candidate was not reported at all"}
}

func Replay(root string, row Recorded) (Outcome, error) {
	files, err := TrackedFiles(root, row.Path)
	if err != nil {
		return Outcome{}, err
	}
	pattern, err := regexp.Compile(row.Pattern)
	if err != nil {
		return Outcome{}, fmt.Errorf("bench/search: %s recorded a pattern that no longer compiles: %w", row.Turn, err)
	}
	result, err := tool.Find(tool.Request{Root: root, Files: files, Pattern: pattern, MaxTokens: row.MaxTokens})
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Row: row, Answered: result.Answered, Attempts: result.Tried}, nil
}

func TrackedFiles(root, under string) ([]string, error) {
	base := filepath.Join(root, filepath.FromSlash(under))
	if _, err := os.Stat(base); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrPathGone, under)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return filesGitKeeps(root, under)
	}
	var files []string
	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != base && ignoredDir(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	}
	if err := filepath.WalkDir(base, walk); err != nil {
		return nil, err
	}
	return files, nil
}

func filesGitKeeps(root, under string) ([]string, error) {
	listed, err := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", cmp.Or(filepath.ToSlash(under), ".")).Output()
	if err != nil {
		return nil, fmt.Errorf("bench/search: git ls-files under %s: %w", root, err)
	}
	var files []string
	for rel := range strings.SplitSeq(strings.TrimSuffix(string(listed), "\x00"), "\x00") {
		dirs := strings.Split(rel, "/")
		if rel == "" || slices.ContainsFunc(dirs[:len(dirs)-1], ignoredDir) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			continue
		}
		files = append(files, rel)
	}
	return files, nil
}

func ignoredDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "rtk" || name == "node_modules"
}
