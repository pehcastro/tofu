package catgrep

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
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
	Start, End, Line   int
	UnitStart, UnitEnd int
	Before, After      string
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

type searched struct {
	walked
	sites []Site
	err   error
}

func Workers() int { return min(runtime.GOMAXPROCS(0), konst.CodemodWorkersMax) }

func search(ctx context.Context, l lang.Lang, paths, words []string, edit editor) ([]File, error) {
	files, err := walk(l, paths)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(files, func(a, b walked) int { return cmp.Compare(b.size, a.size) })
	var next atomic.Int64
	outcomes := make([][]searched, Workers())
	var workers sync.WaitGroup
	for w := range outcomes {
		workers.Go(func() {
			parser := lang.NewParser(l)
			for i := next.Add(1) - 1; i < int64(len(files)) && ctx.Err() == nil; i = next.Add(1) - 1 {
				sites, err := rewriteFile(files[i].path, parser, words, edit)
				if err != nil || len(sites) > 0 {
					outcomes[w] = append(outcomes[w], searched{walked: files[i], sites: sites, err: err})
				}
			}
		})
	}
	workers.Wait()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("stopped after %d ms with files left to search: narrow paths", konst.CodemodRunMillis)
	}
	all := slices.SortedFunc(slices.Values(slices.Concat(outcomes...)), func(a, b searched) int { return walkOrder(a.walked, b.walked) })
	found := make([]File, len(all))
	for i, s := range all {
		if s.err != nil {
			return nil, fmt.Errorf("%s: %w", s.path, s.err)
		}
		found[i] = File{Path: s.path, Sites: s.sites}
	}
	return found, nil
}

func walkOrder(a, b walked) int {
	if a.root != b.root {
		return cmp.Compare(a.root, b.root)
	}
	segmentsA, segmentsB := strings.Split(a.path, string(filepath.Separator)), strings.Split(b.path, string(filepath.Separator))
	return slices.Compare(segmentsA, segmentsB)
}

func rewriteFile(path string, parser lang.Parser, words []string, edit editor) ([]Site, error) {
	source, err := os.ReadFile(path)
	if err != nil || len(source) == 0 || len(source) > konst.CodemodFileBytesSkipped && bytes.Count(source, []byte("\n")) > konst.CodemodFileLinesSkipped {
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
	top, first := root.Node(), 0
	for i := range sites {
		sites[i].UnitStart, sites[i].UnitEnd = 0, len(source)
		if top.ChildCount() == 0 {
			continue
		}
		for first < top.ChildCount()-1 && top.Child(first).End() <= uint32(sites[i].Start) {
			first++
		}
		last := first
		for last < top.ChildCount()-1 && top.Child(last+1).Start() < uint32(sites[i].End) {
			last++
		}
		sites[i].UnitStart = min(int(top.Child(first).Start()), sites[i].Start)
		sites[i].UnitEnd = max(int(top.Child(last).End()), sites[i].End)
	}
	return sites, nil
}

func Files(l lang.Lang, roots []string) ([]string, error) {
	found, err := walk(l, roots)
	slices.SortFunc(found, walkOrder)
	files := make([]string, len(found))
	for i, file := range found {
		files[i] = file.path
	}
	return files, err
}
