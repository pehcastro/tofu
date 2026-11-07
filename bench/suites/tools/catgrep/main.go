package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/tools/code/catgrep/lang"
	"tofu/internal/tools/code/catgrep/metavar"
	"tofu/internal/tools/code/catgrep/pattern"
	"tofu/internal/tools/code/catgrep/replace"
	"tofu/internal/tools/code/catgrep/tree"
)

type corpusCase struct {
	Language    string            `json:"language"`
	Kind        string            `json:"kind"`
	Pattern     string            `json:"pattern"`
	Source      string            `json:"source"`
	Strictness  string            `json:"strictness"`
	Env         map[string]string `json:"env"`
	End         *uint32           `json:"end"`
	Origin      string            `json:"origin"`
	Rewrite     string            `json:"rewrite"`
	RewriteForm string            `json:"rewrite_form"`
	Vars        []boundVar        `json:"vars"`
	Expected    string            `json:"expected"`
	Result      string            `json:"result"`
}

type boundVar struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Multi  bool   `json:"multi"`
}

type corpus struct {
	Language string       `json:"language"`
	Cases    []corpusCase `json:"cases"`
}

func main() {
	only := flag.String("only", "match", "match, replace, template or rule")
	cases := flag.String("cases", filepath.Join("bench", "suites", "tools", "catgrep", "cases"), "the corpus directory")
	scratch := flag.String("scratch", filepath.Join(os.TempDir(), "catgrep"), "where each run copies the corpus")
	show := flag.String("show", "", "print the source and the result of the case with this origin")
	flag.Parse()
	switch *only {
	case "rule":
		fmt.Printf("%s: not ported yet\n", *only)
		return
	case "match", "replace", "template":
	default:
		fmt.Fprintf(os.Stderr, "-only %q is not one of match, replace, template, rule\n", *only)
		os.Exit(2)
	}
	run, err := copyCorpus(*cases, *scratch)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("catgrep %s corpus, read from %s\n\n", *only, run)
	for _, l := range []lang.Lang{lang.Go, lang.Python, lang.Rust, lang.Tsx} {
		if err := report(filepath.Join(run, l.String()+".json"), l, *only, *show); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func copyCorpus(from, scratch string) (string, error) {
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		return "", err
	}
	run, err := os.MkdirTemp(scratch, "run-")
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(run, e.Name()), data, 0o644); err != nil {
			return "", err
		}
	}
	return run, nil
}

func report(path string, l lang.Lang, only, show string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var c corpus
	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var failures []string
	total := 0
	for _, tc := range c.Cases {
		if strings.TrimPrefix(tc.Kind, "non_") != only {
			continue
		}
		total++
		got, reason := check(tc, l)
		if reason != "" {
			failures = append(failures, fmt.Sprintf("  FAIL %s %s %q %q: %s", tc.Origin, tc.Kind, tc.Pattern, tc.Rewrite, reason))
		}
		if tc.Origin == show {
			fmt.Printf("%s\nsource:\n%s\nrewrite:\n%s\nresult:\n%s\n\n", tc.Origin, tc.Source, tc.Rewrite, got)
		}
	}
	if total == 0 {
		return nil
	}
	fmt.Printf("%-7s %d/%d passed\n", c.Language, total-len(failures), total)
	for _, f := range failures {
		fmt.Println(f)
	}
	return nil
}

func check(tc corpusCase, l lang.Lang) (string, string) {
	var got string
	var err error
	switch tc.Kind {
	case "match", "non_match":
		return "", checkMatch(tc, l)
	case "replace":
		got, err = replaced(tc, l)
	case "template":
		got, err = templated(tc, l)
	default:
		return "", fmt.Sprintf("kind %q is not one of match, non_match, replace, template", tc.Kind)
	}
	if err != nil {
		return "", err.Error()
	}
	if got != tc.Expected {
		return got, fmt.Sprintf("got %q, want %q", got, tc.Expected)
	}
	return got, ""
}

func replaced(tc corpusCase, l lang.Lang) (string, error) {
	p, err := pattern.New(tc.Pattern, l)
	if err != nil {
		return "", err
	}
	out, ok, err := replace.First(tc.Source, l, p, tc.Rewrite)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("no node matched")
	}
	if tc.Result != "root_text" {
		return out, nil
	}
	root, err := tree.Parse(out, l)
	if err != nil {
		return "", err
	}
	return root.Node().Text(), nil
}

func templated(tc corpusCase, l lang.Lang) (string, error) {
	env := metavar.NewEnv()
	for _, v := range tc.Vars {
		root, err := tree.Parse(v.Source, l)
		if err != nil {
			return "", err
		}
		if v.Multi {
			env.InsertMulti(v.Name, root.Node().Children())
		} else {
			env.Insert(v.Name, root.Node())
		}
	}
	switch tc.RewriteForm {
	case "text":
		return replace.Text(tc.Rewrite, env)
	case "tree":
		return replace.Tree(tc.Rewrite, l, env)
	}
	return "", fmt.Errorf("rewrite_form %q is not one of text, tree", tc.RewriteForm)
}

func checkMatch(tc corpusCase, l lang.Lang) string {
	p, err := pattern.New(tc.Pattern, l)
	if err != nil {
		return err.Error()
	}
	if tc.Strictness != "" {
		if p.Strictness, err = pattern.ParseStrictness(tc.Strictness); err != nil {
			return err.Error()
		}
	}
	root, err := tree.Parse(tc.Source, l)
	if err != nil {
		return err.Error()
	}
	found, ok := p.Find(root.Node())
	if tc.Kind == "non_match" {
		if ok {
			return fmt.Sprintf("matched %q", found.Node.Text())
		}
		return ""
	}
	if !ok {
		return "no node matched; " + trees(tc, l, root)
	}
	texts := found.Env.Texts()
	for _, name := range slices.Sorted(maps.Keys(tc.Env)) {
		if texts[name] != tc.Env[name] {
			return fmt.Sprintf("%s bound to %q, want %q", name, texts[name], tc.Env[name])
		}
	}
	if tc.End != nil {
		for candidate := range root.Node().PreOrder() {
			if end, ok := p.MatchEnd(candidate); ok {
				if end != *tc.End {
					return fmt.Sprintf("match ends at byte %d, want %d", end, *tc.End)
				}
				return ""
			}
		}
		return "no node has a match end"
	}
	return ""
}

func trees(tc corpusCase, l lang.Lang, source *tree.Root) string {
	goal, err := tree.Parse(l.PreprocessPattern(tc.Pattern), l)
	if err != nil {
		return err.Error()
	}
	return "pattern tree " + strings.TrimSpace(goal.Node().SExpr()) + ", source tree " + strings.TrimSpace(source.Node().SExpr())
}
