package catgrep

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
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
	Path       string
	Sites      []Site
	Source     []byte
	KeepsParse bool
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
	need := literals{words: p.Words()}
	if kind, typed := p.Kind(); typed && len(need.words) > 0 {
		need.split = splitFor(l, l.Grammar().SymbolNames[kind])
	}
	found, err := search(ctx, l, paths, need, func(n tree.Node) (replace.Edit, bool, error) {
		env, matched := p.Match(n)
		if !matched {
			return replace.Edit{}, false, nil
		}
		edit, err := replace.MakeEdit(p, pattern.Match{Node: n, Env: env}, rewrite)
		return edit, true, err
	})
	at, unparsed := p.Unparsed()
	if err != nil || len(found) > 0 || !unparsed {
		return found, err
	}
	before := l.PreprocessPattern(src)[:at]
	row, column := strings.Count(before, "\n"), at-strings.LastIndexByte(before, '\n')-1
	if row == 0 {
		column -= len(l.PreprocessPattern(""))
	}
	return nil, fmt.Errorf("pattern has an error: at line %d column %d it does not parse as %s, and it matched nothing. write it as code that parses on its own", row+1, column+1, l)
}

func RewriteRule(ctx context.Context, raw []byte, rewrite string, l lang.Lang, paths []string) ([]File, error) {
	r, err := rule.New(raw, l)
	if err != nil {
		return nil, err
	}
	return search(ctx, l, paths, literals{}, func(n tree.Node) (replace.Edit, bool, error) {
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
	file File
	err  error
}

func Workers() int { return min(runtime.GOMAXPROCS(0), konst.CodemodWorkersMax) }

type literals struct {
	words []string
	split split
}

type scratch struct {
	read    []byte
	masked  []byte
	cuts    []cut
	hits    []int
	keep    []bool
	pending []tree.Node
	checks  lang.Checks
}

func search(ctx context.Context, l lang.Lang, paths []string, need literals, edit editor) ([]File, error) {
	files, err := walk(l, paths)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(files, func(a, b walked) int { return cmp.Compare(b.size, a.size) })
	var next atomic.Int64
	outcomes := make([][]searched, min(Workers(), len(files)))
	var workers sync.WaitGroup
	for w := range outcomes {
		workers.Go(func() {
			parser := tree.NewParser(l)
			s := scratch{checks: lang.NewChecks(l)}
			for i := next.Add(1) - 1; i < int64(len(files)) && ctx.Err() == nil; i = next.Add(1) - 1 {
				file, err := rewriteFile(files[i], parser, &s, need, edit)
				if err != nil || len(file.Sites) > 0 {
					outcomes[w] = append(outcomes[w], searched{walked: files[i], file: file, err: err})
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
		found[i] = s.file
	}
	return found, nil
}

func walkOrder(a, b walked) int {
	if a.root != b.root {
		return cmp.Compare(a.root, b.root)
	}
	restA, restB, moreA, moreB := a.path, b.path, true, true
	for moreA && moreB {
		var segmentA, segmentB string
		segmentA, restA, moreA = strings.Cut(restA, string(filepath.Separator))
		segmentB, restB, moreB = strings.Cut(restB, string(filepath.Separator))
		if order := strings.Compare(segmentA, segmentB); order != 0 {
			return order
		}
	}
	switch {
	case moreA:
		return 1
	case moreB:
		return -1
	}
	return 0
}

func (s *scratch) readSized(file walked) ([]byte, error) {
	opened, err := os.Open(file.path)
	if err != nil {
		return nil, err
	}
	s.read = slices.Grow(s.read[:0], int(file.size)+1)[:file.size+1]
	n, err := opened.Read(s.read)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err == nil && n != int(file.size) {
		rest := bytes.NewBuffer(s.read[:n])
		_, err = rest.ReadFrom(opened)
		s.read = rest.Bytes()
		n = len(s.read)
	}
	return s.read[:n], errors.Join(err, opened.Close())
}

func rewriteFile(file walked, parser *tree.Parser, s *scratch, need literals, edit editor) (File, error) {
	source, err := s.readSized(file)
	if err != nil || len(source) == 0 || len(source) > konst.CodemodFileBytesSkipped && bytes.Count(source, []byte("\n")) > konst.CodemodFileLinesSkipped {
		return File{}, err
	}
	munch := maximalMunch(parser.Lang)
	for _, word := range need.words {
		if nextWord(source, word, 0, munch && identifierShaped(word)) < 0 {
			return File{}, nil
		}
	}
	searched, regioned := source, false
	host, _ := lang.FromPath(file.path)
	switch {
	case host != parser.Lang:
		var scripted bool
		if searched, scripted, err = lang.Scripts(source, host, parser.Lang); err != nil || !scripted {
			return File{}, err
		}
	case need.split != unsplit:
		masked, held := s.mask(parser.Lang, source, need)
		if !held {
			return File{}, nil
		}
		if masked != nil {
			searched, regioned = masked, true
		}
	}
	root, err := parser.Parse(searched)
	if err == nil && regioned && !root.Clean() {
		root.Release()
		searched = source
		root, err = parser.Parse(source)
	}
	if err != nil {
		return File{}, err
	}
	defer root.Release()
	var sites []Site
	line, counted := 1, 0
	pending := s.pending[:0]
	pending = append(pending, root.Node())
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		made, matched, err := edit(node)
		if err != nil {
			return File{}, err
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
	s.pending = pending
	clear(pending[:cap(pending)])
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
	if len(sites) == 0 || parser.Lang != lang.Go {
		return File{Path: file.path, Sites: sites}, nil
	}
	if !root.Faulted(s.checks) && tokensKept(searched, source, sites, parser.Lang.Grammar().SymbolNames) {
		return File{Path: file.path, Sites: sites, KeepsParse: true}, nil
	}
	return File{Path: file.path, Sites: sites, Source: bytes.Clone(source)}, nil
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
