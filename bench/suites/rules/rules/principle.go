package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/txtar"
)

type principle struct {
	name  string
	leaks []string
}

func principles() []principle {
	return []principle{
		{"principle-build-the-lever", []string{"codemod", "script", "generator", "sed ", "gofmt -r", "rewrite tool", "lever"}},
		{"principle-make-operations-idempotent", []string{"idempot", "converge", "reconcil", "twice", "same end state", "stale lock"}},
		{"principle-model-the-domain", []string{"state machine", "enum", "discriminated", "union", "unrepresentable", "lookup table", "registry", "model the domain"}},
		{"principle-separate-before-serializing-shared-state", []string{"mutex", "lock", "separate", "own file", "per-worker", "per-instance", "serializ", "shard"}},
		{"principle-sequence-verifiable-units", []string{"step by step", "one at a time", "after each", "each step", "small units", "incremental", "red to green"}},
	}
}

type arm string

const (
	armOn  arm = "rule-on"
	armOff arm = "rule-off"
)

type task struct {
	name   string
	prompt string
	seed   []txtar.File
	check  []txtar.File
	leaked string
}

type principleSet struct {
	principle
	rule  string
	tasks []task
}

func (s principleSet) kept() int {
	kept := 0
	for _, t := range s.tasks {
		if t.leaked == "" {
			kept++
		}
	}
	return kept
}

const checkPrefix = "check/"

func loadSets(root string) ([]principleSet, error) {
	var sets []principleSet
	for _, p := range principles() {
		rule, err := os.ReadFile(filepath.Join(root, p.name, "rule.md"))
		if err != nil {
			return nil, fmt.Errorf("%s has no candidate rule text: %w", p.name, err)
		}
		paths, err := filepath.Glob(filepath.Join(root, p.name, "*.txtar"))
		if err != nil {
			return nil, err
		}
		sort.Strings(paths)
		set := principleSet{principle: p, rule: strings.TrimSpace(string(rule))}
		for _, path := range paths {
			t, err := parseTask(path, p.leaks)
			if err != nil {
				return nil, err
			}
			set.tasks = append(set.tasks, t)
		}
		sets = append(sets, set)
	}
	return sets, nil
}

func parseTask(path string, leaks []string) (task, error) {
	archive, err := txtar.ParseFile(path)
	if err != nil {
		return task{}, err
	}
	t := task{name: strings.TrimSuffix(filepath.Base(path), ".txtar"), prompt: strings.TrimSpace(string(archive.Comment))}
	visible := strings.ToLower(t.prompt)
	for _, f := range archive.Files {
		if strings.HasPrefix(f.Name, checkPrefix) {
			t.check = append(t.check, f)
			continue
		}
		t.seed = append(t.seed, f)
		visible += "\n" + strings.ToLower(string(f.Data))
	}
	if t.prompt == "" || len(t.check) == 0 || len(t.seed) == 0 {
		return task{}, fmt.Errorf("%s needs a prompt, a seed project and a %s file", path, checkPrefix)
	}
	for _, term := range leaks {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(term)).MatchString(visible) {
			t.leaked = term
			break
		}
	}
	return t, nil
}

func printLeakage(sets []principleSet) {
	for _, set := range sets {
		var thrown []string
		for _, t := range set.tasks {
			if t.leaked != "" {
				thrown = append(thrown, fmt.Sprintf("%s names %q", t.name, t.leaked))
			}
		}
		fmt.Printf("leakage %s: %d tasks, %d thrown out, prompt and seed searched for %q", set.name, len(set.tasks), len(thrown), set.leaks)
		if len(thrown) > 0 {
			fmt.Printf(": %s", strings.Join(thrown, "; "))
		}
		fmt.Println()
	}
}
