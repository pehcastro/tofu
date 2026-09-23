package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tofu/bench/harness/task"
)

type Seed struct {
	Path   string `json:"path"`
	Files  int    `json:"files"`
	Digest string `json:"digest"`
}

func (s Seed) Line() string {
	if s.Path == "" {
		return "no seed was recorded beside this row, so nobody can say which tree the arm started from"
	}
	return fmt.Sprintf("%s, %d files, %s", s.Path, s.Files, s.Digest)
}

func (s Seed) Check(dir string) error {
	got, err := digestTree(dir)
	if err != nil {
		return err
	}
	if got.Digest == s.Digest {
		return nil
	}
	return fmt.Errorf("%s holds %d files hashing %s and the seed %s holds %d hashing %s, so this tree is not what the task starts from and the run is refused rather than recorded",
		dir, got.Files, got.Digest, s.Path, s.Files, s.Digest)
}

func SeedOf(root string, version int) (Seed, error) {
	if _, err := task.Of(version); err != nil {
		return Seed{}, err
	}
	seed, err := digestTree(task.Path(root, version, task.Seed))
	if err != nil {
		return Seed{}, fmt.Errorf("the v%d seed is the tree every arm starts from, and it could not be read: %w", version, err)
	}
	seed.Path = filepath.ToSlash(task.Path("", version, task.Seed))
	return seed, nil
}

func Stage(root string, version int, armDir string) (Seed, error) {
	seed, err := SeedOf(root, version)
	if err != nil {
		return Seed{}, err
	}
	target, err := filepath.Abs(armDir)
	if err != nil {
		return Seed{}, err
	}
	if !strings.EqualFold(filepath.Base(filepath.Dir(target)), playgroundRoot) {
		return Seed{}, fmt.Errorf("staging deletes %s and writes the seed in its place, and only a direct child of a %s directory may be deleted that way",
			target, playgroundRoot)
	}
	if err := os.RemoveAll(target); err != nil {
		return Seed{}, err
	}
	if err := copyTree(task.Path(root, version, task.Seed), target); err != nil {
		return Seed{}, err
	}
	if err := seed.Check(target); err != nil {
		return Seed{}, err
	}
	return seed, nil
}

func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o600)
	})
}

var unseeded = map[string]bool{".git": true, "node_modules": true, ".tofu": true}

func digestTree(dir string) (Seed, error) {
	var lines []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != dir && unseeded[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		lines = append(lines, filepath.ToSlash(relative)+" "+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		return Seed{}, err
	}
	sort.Strings(lines)
	whole := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return Seed{Files: len(lines), Digest: hex.EncodeToString(whole[:])}, nil
}
