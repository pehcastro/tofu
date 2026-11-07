package catgrep

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"tofu/internal/konst"
	"tofu/internal/tools/code/catgrep/lang"
	"tofu/internal/tools/code/catgrep/pattern"
	"tofu/internal/tools/code/catgrep/replace"
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

func Rewrite(ctx context.Context, src, rewrite string, l lang.Lang, paths []string) ([]File, error) {
	p, err := pattern.New(src, l)
	if err != nil {
		return nil, err
	}
	files, err := Files(l, paths)
	if err != nil {
		return nil, err
	}
	searched := make([]File, len(files))
	failures := make([]error, len(files))
	var next atomic.Int64
	var workers sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		workers.Go(func() {
			for i := int(next.Add(1) - 1); i < len(files) && ctx.Err() == nil; i = int(next.Add(1) - 1) {
				searched[i].Path = files[i]
				searched[i].Sites, failures[i] = rewriteFile(files[i], l, p, rewrite)
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
	if len(found) > 0 {
		return found, nil
	}
	parsed, err := lang.Parse([]byte(l.PreprocessPattern(src)), l)
	if err != nil {
		return nil, err
	}
	fault, broken := lang.FirstFault(parsed)
	if !broken {
		return nil, nil
	}
	problem := "does not parse"
	if fault.Missing {
		problem = "is missing a " + fault.Kind
	}
	column := int(fault.At.Column)
	if fault.At.Row == 0 {
		column -= len(l.PreprocessPattern(""))
	}
	return nil, fmt.Errorf("pattern has an error: at line %d column %d it %s as %s, and it matched nothing. write it as code that parses on its own", fault.At.Row+1, column+1, problem, l)
}

func rewriteFile(path string, l lang.Lang, p pattern.Pattern, rewrite string) ([]Site, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	searched := source
	if host, _ := lang.FromPath(path); host != l {
		var scripted bool
		if searched, scripted, err = lang.Scripts(source, host, l); err != nil || !scripted {
			return nil, err
		}
	}
	root, err := tree.Parse(string(searched), l)
	if err != nil {
		return nil, err
	}
	var sites []Site
	pending := []tree.Node{root.Node()}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		env, matched := p.Match(node)
		if !matched {
			children := node.Children()
			for i := len(children) - 1; i >= 0; i-- {
				pending = append(pending, children[i])
			}
			continue
		}
		edit, err := replace.MakeEdit(p, pattern.Match{Node: node, Env: env}, rewrite)
		if err != nil {
			return nil, err
		}
		sites = append(sites, Site{
			Start:  int(edit.Start),
			End:    int(edit.End),
			Line:   bytes.Count(source[:edit.Start], []byte("\n")) + 1,
			Before: string(source[edit.Start:edit.End]),
			After:  edit.Text,
		})
	}
	return sites, nil
}

func Files(l lang.Lang, roots []string) ([]string, error) {
	var files []string
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
			files = append(files, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}
