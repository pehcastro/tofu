package search

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	tool "tofu/internal/search"
)

const (
	unitPatternsPerCorpus = 30
	framedSuffix          = ".framed"
)

type unitCorpus struct {
	name, root, under string
	extensions        []string
}

type armCost struct {
	tokens, returned, matched, fallbacks, reads, readTokens int
}

func TestWhatAWholeUnitSavesOverFramedLinesOutsideGo(t *testing.T) {
	sources := filepath.Join(treeRoot, ".local", "sources")
	for _, corpus := range []unitCorpus{
		{name: "python", root: filepath.Join(sources, "aider"), under: "aider", extensions: []string{".py"}},
		{name: "typescript", root: filepath.Join(sources, "opencode-dev"), under: "packages/opencode/src", extensions: []string{".ts", ".tsx"}},
		{name: "rust", root: filepath.Join(sources, "goose"), under: "crates", extensions: []string{".rs"}},
	} {
		listed, err := TrackedFiles(corpus.root, corpus.under)
		if err != nil {
			t.Logf("%s: skipped, %v", corpus.name, err)
			continue
		}
		files := slices.DeleteFunc(listed, func(rel string) bool { return !slices.Contains(corpus.extensions, filepath.Ext(rel)) })
		framedRoot := t.TempDir()
		framedFiles := make([]string, len(files))
		for index, rel := range files {
			body, err := os.ReadFile(filepath.Join(corpus.root, rel))
			if err != nil {
				t.Fatal(err)
			}
			framedFiles[index] = rel + framedSuffix
			target := filepath.Join(framedRoot, filepath.FromSlash(framedFiles[index]))
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, body, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		var framed, whole armCost
		names := calledNames(t, corpus.root, files)
		for _, name := range names {
			pattern := regexp.MustCompile(`\b` + name + `\(`)
			find := func(root string, files []string, budget int) tool.Result {
				result, err := tool.Find(tool.Request{Root: root, Files: files, Pattern: pattern, MaxTokens: budget})
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			declared := find(corpus.root, files, 1<<30).Units
			for arm, result := range map[*armCost]tool.Result{&framed: find(framedRoot, framedFiles, 0), &whole: find(corpus.root, files, 0)} {
				arm.tokens += result.Stats.Tokens
				arm.returned += result.Stats.Returned
				arm.matched += result.Stats.Units
				arm.fallbacks += result.Stats.Fallbacks
				read := map[string]bool{}
				for _, shown := range result.Units {
					for _, decl := range readsAfter(shown, declared) {
						key := fmt.Sprint(decl.Path, decl.FirstLine)
						if read[key] {
							continue
						}
						read[key] = true
						arm.reads++
						arm.readTokens += decl.Tokens
					}
				}
			}
		}
		for _, arm := range []struct {
			label string
			cost  armCost
		}{{"framed lines (today)", framed}, {"whole units", whole}} {
			t.Logf("%s, %d files, %d searches, %s: %d tokens returned, %d of %d units returned, %d of them from a file read as unparsed, %d follow-up reads costing %d tokens, %d tokens in all",
				corpus.name, len(files), len(names), arm.label, arm.cost.tokens, arm.cost.returned, arm.cost.matched, arm.cost.fallbacks, arm.cost.reads, arm.cost.readTokens, arm.cost.tokens+arm.cost.readTokens)
		}
	}
}

func calledNames(t *testing.T, root string, files []string) []string {
	t.Helper()
	call := regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]{5,})\(`)
	filesCalling := map[string]int{}
	for _, rel := range files {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, match := range call.FindAllStringSubmatch(string(body), -1) {
			if !seen[match[1]] {
				seen[match[1]] = true
				filesCalling[match[1]]++
			}
		}
	}
	var names []string
	for name, count := range filesCalling {
		if count >= 2 && count <= 4 {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	step := max(1, len(names)/unitPatternsPerCorpus)
	var sampled []string
	for index := 0; index < len(names) && len(sampled) < unitPatternsPerCorpus; index += step {
		sampled = append(sampled, names[index])
	}
	return sampled
}

func readsAfter(shown tool.Unit, declared []tool.Unit) []tool.Unit {
	if shown.Kind != tool.KindLines {
		return nil
	}
	var reads []tool.Unit
	for _, decl := range declared {
		if decl.Kind == tool.KindLines || decl.Path != strings.TrimSuffix(shown.Path, framedSuffix) {
			continue
		}
		for _, match := range shown.Matches {
			if decl.FirstLine <= match.Line && match.Line <= decl.LastLine && (decl.FirstLine < shown.FirstLine || decl.LastLine > shown.LastLine) {
				reads = append(reads, decl)
				break
			}
		}
	}
	return reads
}
