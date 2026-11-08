package catgrep

import (
	"bytes"
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"tofu/internal/konst"
	"tofu/internal/tools/code/catgrep/lang"
)

type walked struct {
	root int
	path string
	size int64
}

type ignoreRule struct {
	literal    string
	suffix     string
	expression *regexp.Regexp
	nameOnly   bool
	negated    bool
	dirOnly    bool
}

type ignores struct {
	outer *ignores
	base  string
	rules []ignoreRule
}

type walker struct {
	lang    lang.Lang
	slots   chan struct{}
	pending sync.WaitGroup
	found   sync.Mutex
	files   []walked
	err     error
}

func walk(l lang.Lang, roots []string) ([]walked, error) {
	w := &walker{lang: l, slots: make(chan struct{}, Workers())}
	for i, root := range roots {
		root, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			w.descend(i, root, outerIgnores(root))
		} else {
			w.offer(i, root, info)
		}
	}
	w.pending.Wait()
	return w.files, w.err
}

func (w *walker) halted() bool {
	w.found.Lock()
	defer w.found.Unlock()
	return w.err != nil
}

func (w *walker) failed(err error) {
	w.found.Lock()
	defer w.found.Unlock()
	w.err = cmp.Or(w.err, err)
}

func (w *walker) offer(root int, path string, info fs.FileInfo) {
	fileLang, parsed := lang.FromPath(info.Name())
	if !parsed || fileLang != w.lang && (!w.lang.Embeddable() || !fileLang.HostsScripts()) {
		return
	}
	w.found.Lock()
	defer w.found.Unlock()
	if len(w.files) == konst.CodemodFilesScanned {
		w.err = cmp.Or(w.err, fmt.Errorf("more than %d %s files under paths: narrow paths", konst.CodemodFilesScanned, w.lang))
		return
	}
	w.files = append(w.files, walked{root: root, path: path, size: info.Size()})
}

func (w *walker) descend(root int, dir string, scope *ignores) {
	select {
	case w.slots <- struct{}{}:
		w.pending.Go(func() {
			w.dir(root, dir, scope)
			<-w.slots
		})
	default:
		w.dir(root, dir, scope)
	}
}

func (w *walker) dir(root int, dir string, scope *ignores) {
	if w.halted() {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		w.failed(err)
		return
	}
	for _, name := range []string{".gitignore", ".ignore"} {
		if hasEntry(entries, name) {
			scope = readIgnores(scope, dir, name)
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		full := filepath.Join(dir, name)
		switch {
		case entry.IsDir() && strings.HasPrefix(name, "."), scope.ignored(full, name, entry.IsDir()):
		case !entry.IsDir():
			info, err := entry.Info()
			if err != nil {
				w.failed(err)
				return
			}
			w.offer(root, full, info)
		case name != "vendor" && name != "testdata" && name != "node_modules":
			w.descend(root, full, scope)
		}
	}
}

func hasEntry(entries []fs.DirEntry, name string) bool {
	for _, entry := range entries {
		if entry.Name() == name {
			return true
		}
	}
	return false
}

func outerIgnores(root string) *ignores {
	var chain []string
	for dir := root; ; dir = filepath.Dir(dir) {
		chain = append(chain, dir)
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			break
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
	repo := chain[len(chain)-1]
	scope := readIgnores(nil, repo, filepath.Join(".git", "info", "exclude"))
	for i := len(chain) - 1; i > 0; i-- {
		scope = readIgnores(readIgnores(scope, chain[i], ".gitignore"), chain[i], ".ignore")
	}
	return scope
}

func readIgnores(outer *ignores, dir, name string) *ignores {
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil || len(body) > konst.IgnoreFileBytesCap {
		return outer
	}
	scope := &ignores{outer: outer, base: dir}
	for line := range bytes.Lines(body) {
		if rule, ok := ignoreLine(strings.TrimRight(string(line), "\r\n \t")); ok {
			scope.rules = append(scope.rules, rule)
		}
	}
	if len(scope.rules) == 0 {
		return outer
	}
	return scope
}

func ignoreLine(line string) (ignoreRule, bool) {
	if line == "" || line[0] == '#' {
		return ignoreRule{}, false
	}
	pattern, negated := strings.CutPrefix(line, "!")
	pattern = strings.TrimPrefix(pattern, `\`)
	pattern, dirOnly := strings.CutSuffix(pattern, "/")
	rule := ignoreRule{nameOnly: !strings.Contains(pattern, "/"), negated: negated, dirOnly: dirOnly}
	pattern = strings.TrimPrefix(pattern, "/")
	switch {
	case pattern == "":
		return ignoreRule{}, false
	case !strings.ContainsAny(pattern, "*?["):
		rule.literal = pattern
		return rule, true
	case len(pattern) > 1 && pattern[0] == '*' && !strings.ContainsAny(pattern[1:], "*?[/"):
		rule.suffix = pattern[1:]
		return rule, true
	}
	var built strings.Builder
	built.WriteString("^")
	segments := strings.Split(pattern, "/")
	for i, segment := range segments {
		last := i == len(segments)-1
		switch {
		case segment == "**" && last:
			built.WriteString(".*")
		case segment == "**":
			built.WriteString("(?:[^/]+/)*")
		default:
			built.WriteString(globSegment(segment))
			if !last {
				built.WriteString("/")
			}
		}
	}
	expression, err := regexp.Compile(built.String() + "$")
	rule.expression = expression
	return rule, err == nil
}

func globSegment(segment string) string {
	var built strings.Builder
	for i := 0; i < len(segment); i++ {
		switch c := segment[i]; c {
		case '*':
			built.WriteString("[^/]*")
		case '?':
			built.WriteString("[^/]")
		case '[':
			end := strings.IndexByte(segment[i:], ']')
			if end < 0 {
				built.WriteString(`\[`)
				continue
			}
			members, negated := strings.CutPrefix(segment[i+1:i+end], "!")
			if negated {
				members = "^" + members
			}
			built.WriteString("[" + members + "]")
			i += end
		default:
			built.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return built.String()
}

func (s *ignores) ignored(full, name string, isDir bool) bool {
	for scope := s; scope != nil; scope = scope.outer {
		relative := ""
		for i := len(scope.rules) - 1; i >= 0; i-- {
			rule := &scope.rules[i]
			if rule.dirOnly && !isDir {
				continue
			}
			subject := name
			if !rule.nameOnly {
				if relative == "" {
					relative = filepath.ToSlash(strings.TrimPrefix(full, scope.base+string(filepath.Separator)))
				}
				subject = relative
			}
			if rule.matches(subject) {
				return !rule.negated
			}
		}
	}
	return false
}

func (r *ignoreRule) matches(subject string) bool {
	switch {
	case r.literal != "":
		return subject == r.literal
	case r.suffix != "":
		return strings.HasSuffix(subject, r.suffix)
	}
	return r.expression.MatchString(subject)
}
