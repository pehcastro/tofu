package catgrep

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/tools/code/catgrep/lang"
	"tofu/internal/tools/code/catgrep/pattern"
	"tofu/internal/tools/code/catgrep/replace"
	"tofu/internal/tools/code/catgrep/tree"
)

const Languages = "go, typescript (ts), tsx, python (py), rust (rs)"

type Site struct {
	Start, End, Line int
	Before, After    string
}

type File struct {
	Path  string
	Sites []Site
}

func LangNamed(name string) (lang.Lang, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "go", "golang":
		return lang.Go, nil
	case "ts", "typescript":
		return lang.TypeScript, nil
	case "tsx":
		return lang.Tsx, nil
	case "py", "python":
		return lang.Python, nil
	case "rs", "rust":
		return lang.Rust, nil
	}
	return 0, fmt.Errorf("lang %q is not one cat-grep parses: use one of %s", name, Languages)
}

func Rewrite(ctx context.Context, src, rewrite string, l lang.Lang, paths []string) ([]File, error) {
	p, err := pattern.New(src, l)
	if err != nil {
		return nil, err
	}
	files, err := walk(l, paths)
	if err != nil {
		return nil, err
	}
	var found []File
	for _, path := range files {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("stopped after %d ms with files left to search: narrow paths", konst.CodemodRunMillis)
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sites, err := rewriteFile(source, l, p, rewrite)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if len(sites) > 0 {
			found = append(found, File{Path: path, Sites: sites})
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
	return nil, fmt.Errorf("pattern has an error: at line %d column %d it %s as %s, and it matched nothing. write it as code that parses on its own", fault.At.Row+1, fault.At.Column+1, problem, l)
}

func rewriteFile(source []byte, l lang.Lang, p pattern.Pattern, rewrite string) ([]Site, error) {
	root, err := tree.Parse(string(source), l)
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

func walk(l lang.Lang, roots []string) ([]string, error) {
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
			case entry.IsDir() || !parsed || fileLang != l:
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
