package rule

import (
	"io/fs"
	"path/filepath"
	"time"

	"tofu/internal/sys"
)

var proseExtensions = map[string]bool{".go": true, ".md": true}

func CheckTree(rules []Rule, checkers map[string]Checker, root string, now func() time.Time) ([]Fire, error) {
	var fires []Fire
	run := func(subject Subject, a Artifact, target string) error {
		for _, r := range rules {
			if checkers[r.Checker].Subject != subject {
				continue
			}
			fire, err := Run(r, checkers, a, target, now())
			if err != nil {
				return err
			}
			if len(fire.Findings) > 0 {
				fires = append(fires, fire)
			}
		}
		return nil
	}
	err := sys.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipTreeDir(d.Name()) {
				return filepath.SkipDir
			}
			return run(SubjectGoPackage, GoPackage{Dir: path}, path)
		}
		ext := filepath.Ext(path)
		if ext == ".go" {
			if err := run(SubjectGoFile, GoFile{Path: path}, path); err != nil {
				return err
			}
		}
		if !proseExtensions[ext] {
			return nil
		}
		return run(SubjectTextFile, TextFile{Path: path}, path)
	})
	if err != nil {
		return nil, err
	}
	return fires, nil
}

func skipTreeDir(name string) bool {
	switch name {
	case ".git", sys.StateDirName, sys.LegacyStateDirName, "node_modules", "vendor":
		return true
	}
	return false
}
