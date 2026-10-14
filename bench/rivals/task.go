package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type hiddenTest struct {
	Part string `json:"part"`
	Into string `json:"into"`
}

type task struct {
	Name     string       `json:"-"`
	Hidden   []hiddenTest `json:"hidden"`
	Gone     []string     `json:"gone"`
	Expected []string     `json:"expected"`
	Answers  []string     `json:"answers"`
	Sea      *seaSpec     `json:"sea"`
}

func unleakedTask(name, promptOverride string) (task, string, error) {
	t := task{Name: name}
	body, err := os.ReadFile(filepath.Join(t.dir(), "task.json"))
	if err != nil {
		return t, "", err
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return t, "", err
	}
	prompt, err := os.ReadFile(cmp.Or(promptOverride, filepath.Join(t.dir(), "prompt.txt")))
	if err != nil {
		return t, "", err
	}
	return t, strings.TrimSpace(string(prompt)), t.leakCheck(string(prompt))
}

func (t task) dir() string {
	if t.Name == "" {
		return filepath.Join(here(), "testdata")
	}
	return filepath.Join(here(), "testdata", "tasks", t.Name)
}

func taskNames() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(here(), "testdata", "tasks"))
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, err
}

func (t task) leakCheck(prompt string) error {
	var named []string
	for _, answer := range t.Answers {
		if regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(answer) + `($|[^A-Za-z0-9_])`).MatchString(prompt) {
			named = append(named, answer)
		}
	}
	if len(named) > 0 {
		return fmt.Errorf("leakage: the prompt names its answer: %s", strings.Join(named, ", "))
	}
	fmt.Printf("leakage: the prompt names none of the %d answers (%s)\n", len(t.Answers), strings.Join(t.Answers, ", "))
	return nil
}

func (t task) expects(file string) bool {
	return slices.ContainsFunc(t.Expected, func(pattern string) bool {
		matched, _ := path.Match(pattern, file)
		return matched
	})
}

func copyTree(source, repo string) error {
	return filepath.WalkDir(source, func(file string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		target := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
}
