package catgrep

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"tofu/internal/konst"
	"tofu/internal/tools/code/catgrep/lang"
	"tofu/internal/tools/code/catgrep/pattern"
	"tofu/internal/tools/code/catgrep/replace"
	"tofu/internal/tools/code/catgrep/rule"
	"tofu/internal/tools/code/catgrep/tree"
)

const Languages = "the language name or its file extension, such as go, ts, tsx, js, py, rs, php, java, cs, cpp, rb, swift, kt; a script or style inside an html, svelte or vue file is searched with js, ts or css"

type Site struct {
	Start, End, Line int
	Before, After    string
}

type File struct {
	Path  string
	Sites []Site
}

func LangNamed(name string) (lang.Lang, error) {
	if l, ok := lang.Named(name); ok {
		return l, nil
	}
	return 0, fmt.Errorf("lang %q is not one cat-grep parses: use one of %s", name, lang.Listing())
}

type editor func(n tree.Node) (replace.Edit, bool, error)

func Rewrite(ctx context.Context, src, rewrite string, l lang.Lang, paths []string) ([]File, error) {
	p, err := pattern.New(src, l)
	if err != nil {
		return nil, err
	}
	found, err := search(ctx, l, paths, p.Words(), func(n tree.Node) (replace.Edit, bool, error) {
		env, matched := p.Match(n)
		if !matched {
			return replace.Edit{}, false, nil
		}
		edit, err := replace.MakeEdit(p, pattern.Match{Node: n, Env: env}, rewrite)
		return edit, true, err
	})
	if err != nil || len(found) > 0 {
		return found, err
	}
	parsed, err := lang.Parse([]byte(l.PreprocessPattern(src)), l)
	if err != nil {
		return nil, err
	}
	defer parsed.Release()
	fault, broken := lang.FirstFault(parsed)
	if !broken || fault.Missing {
		return nil, nil
	}
	column := int(fault.At.Column)
	if fault.At.Row == 0 {
		column -= len(l.PreprocessPattern(""))
	}
	return nil, fmt.Errorf("pattern has an error: at line %d column %d it does not parse as %s, and it matched nothing. write it as code that parses on its own", fault.At.Row+1, column+1, l)
}

func RewriteRule(ctx context.Context, raw []byte, rewrite string, l lang.Lang, paths []string) ([]File, error) {
	r, err := rule.New(raw, l)
	if err != nil {
		return nil, err
	}
	return search(ctx, l, paths, nil, func(n tree.Node) (replace.Edit, bool, error) {
		env, matched := r.Match(n)
		if !matched {
			return replace.Edit{}, false, nil
		}
		text, err := replace.Text(rewrite, env)
		indent := replace.IndentAt(n.Source()[:n.Start()])
		return replace.Edit{Start: n.Start(), End: n.End(), Text: replace.Reindent(text, 0, indent)}, true, err
	})
}

func search(ctx context.Context, l lang.Lang, paths, words []string, edit editor) ([]File, error) {
	files, sizes, err := walk(l, paths)
	if err != nil {
		return nil, err
	}
	largestFirst := make([]int, len(files))
	for i := range largestFirst {
		largestFirst[i] = i
	}
	slices.SortFunc(largestFirst, func(a, b int) int { return cmp.Compare(sizes[b], sizes[a]) })
	searched := make([]File, len(files))
	failures := make([]error, len(files))
	var next atomic.Int64
	var workers sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(files)) {
		workers.Go(func() {
			parser := lang.NewParser(l)
			for at := int(next.Add(1) - 1); at < len(files) && ctx.Err() == nil; at = int(next.Add(1) - 1) {
				i := largestFirst[at]
				searched[i].Path = files[i]
				searched[i].Sites, failures[i] = rewriteFile(files[i], parser, words, edit)
			}
		})
	}
	workers.Wait()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("stopped after %d ms with files left to search: narrow paths", konst.CodemodRunMillis)
	}
	var found []File
	for i, file := range searched {
		if failures[i] != nil {
			return nil, fmt.Errorf("%s: %w", file.Path, failures[i])
		}
		if len(file.Sites) > 0 {
			found = append(found, file)
		}
	}
	return found, nil
}

func rewriteFile(path string, parser lang.Parser, words []string, edit editor) ([]Site, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for _, word := range words {
		if !bytes.Contains(source, []byte(word)) {
			return nil, nil
		}
	}
	searched := source
	if host, _ := lang.FromPath(path); host != parser.Lang {
		var scripted bool
		if searched, scripted, err = lang.Scripts(source, host, parser.Lang); err != nil || !scripted {
			return nil, err
		}
	}
	root, err := tree.ParseWith(parser, searched)
	if err != nil {
		return nil, err
	}
	defer root.Release()
	var sites []Site
	line, counted := 1, 0
	pending := []tree.Node{root.Node()}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		made, matched, err := edit(node)
		if err != nil {
			return nil, err
		}
		for i := node.ChildCount() - 1; i >= 0; i-- {
			pending = append(pending, node.Child(i))
		}
		if !matched {
			continue
		}
		line += bytes.Count(source[counted:made.Start], []byte("\n"))
		counted = int(made.Start)
		sites = append(sites, Site{
			Start:  int(made.Start),
			End:    int(made.End),
			Line:   line,
			Before: string(source[made.Start:made.End]),
			After:  made.Text,
		})
	}
	return sites, nil
}

func Files(l lang.Lang, roots []string) ([]string, error) {
	files, _, err := walk(l, roots)
	return files, err
}

func walk(l lang.Lang, roots []string) ([]string, []int64, error) {
	var files []string
	var sizes []int64
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := entry.Name()
			fileLang, parsed := lang.FromPath(name)
			switch {
			case entry.IsDir() && path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" || name == "node_modules"):
				return filepath.SkipDir
			case entry.IsDir() || !parsed || fileLang != l && (!l.Embeddable() || !fileLang.HostsScripts()):
				return nil
			case len(files) == konst.CodemodFilesScanned:
				return fmt.Errorf("more than %d %s files under paths: narrow paths", konst.CodemodFilesScanned, l)
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			files, sizes = append(files, path), append(sizes, info.Size())
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return files, sizes, nil
}
