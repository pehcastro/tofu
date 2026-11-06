package test

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/tools/code/typecheck/checkers"
)

func nodeTests(root, resolved string, isDir bool, name string) (testCommand, string, bool) {
	var manifest checkers.PackageManifest
	if body, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		_ = json.Unmarshal(body, &manifest)
	}
	scripted := regexp.MustCompile(`\bnode(?:\.exe)?\s(?:[^&|;]*\s)?--test\b`).MatchString(manifest.Scripts["test"])
	relative, _ := filepath.Rel(root, resolved)
	relative = filepath.ToSlash(relative)
	files := []string{relative}
	base := filepath.Base(resolved)
	switch {
	case isDir && !scripted:
		return testCommand{}, "", false
	case isDir:
		files = []string{path.Join(relative, "**/*.test.*"), path.Join(relative, "**/*.spec.*")}
	case !strings.Contains(base, ".test.") && !strings.Contains(base, ".spec."):
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		beside, _ := colocatedTests(filepath.Dir(resolved), stem)
		if len(beside) == 0 && scripted {
			return testCommand{}, "test: no test for " + base + ": looked for " + stem + ".test" + filepath.Ext(base) + " or " + stem + ".spec" + filepath.Ext(base) + " beside it", true
		}
		files = nil
		for _, file := range beside {
			rel, _ := filepath.Rel(root, file)
			files = append(files, filepath.ToSlash(rel))
		}
	}
	if !scripted && !slices.ContainsFunc(files, func(file string) bool { return importsNodeTest(filepath.Join(root, filepath.FromSlash(file))) }) {
		return testCommand{}, "", false
	}
	argv := []string{"node", "--test", "--test-reporter=tap"}
	if name != "" {
		argv = append(argv, "--test-name-pattern=^"+name+"$")
	}
	return testCommand{
		runner:  "node --test",
		argv:    append(argv, files...),
		dir:     root,
		install: "install Node.js from https://nodejs.org and put it on PATH",
		parse:   func(output string) checkers.TestRun { return nodeTap(root, output) },
	}, "", true
}

func importsNodeTest(file string) bool {
	body, err := os.ReadFile(file)
	return err == nil && regexp.MustCompile(`from\s+["']node:test["']|require\(\s*["']node:test["']\s*\)`).Match(body)
}

func nodeTap(root, output string) checkers.TestRun {
	result := regexp.MustCompile(`^\s*(not ok|ok) \d+ - (.*?)(?: # (SKIP|TODO)\b.*)?$`)
	tally := regexp.MustCompile(`^# (pass|fail|cancelled|skipped) (\d+)$`)
	var run checkers.TestRun
	failing, errorIndent := -1, 0
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if count := tally.FindStringSubmatch(line); count != nil {
			run.Listed = true
			n, _ := strconv.Atoi(count[2])
			switch count[1] {
			case "pass":
				run.Passed = n
			case "skipped":
				run.Skipped = n
			default:
				run.Failed += n
			}
			continue
		}
		if match := result.FindStringSubmatch(line); match != nil {
			failing, errorIndent = -1, 0
			switch {
			case match[3] == "SKIP":
				run.SkippedNames = append(run.SkippedNames, match[2])
			case match[1] == "not ok" && match[3] == "":
				run.Failures = append(run.Failures, checkers.TestFailure{Name: match[2]})
				failing = len(run.Failures) - 1
			}
			continue
		}
		if failing < 0 {
			continue
		}
		location, located := strings.CutPrefix(trimmed, "location: ")
		switch {
		case located:
			at := strings.ReplaceAll(strings.Trim(location, "'"), `\\`, `\`)
			if colon := strings.LastIndex(at, ":"); colon > 0 {
				at = at[:colon]
			}
			if rel, err := filepath.Rel(root, at); err == nil && !strings.HasPrefix(rel, "..") {
				at = filepath.ToSlash(rel)
			}
			run.Failures[failing].At = at
		case strings.HasPrefix(trimmed, "error: "):
			errorIndent = indent + 1
			if said := strings.Trim(strings.TrimPrefix(trimmed, "error: "), "'|-"); said != "" {
				run.Failures[failing].Said = append(run.Failures[failing].Said, said)
			}
		case trimmed == "":
		case errorIndent > 0 && indent >= errorIndent:
			run.Failures[failing].Said = append(run.Failures[failing].Said, trimmed)
		default:
			errorIndent = 0
		}
	}
	return run
}
