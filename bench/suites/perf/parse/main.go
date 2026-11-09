package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	ts "github.com/odvcencio/gotreesitter"
	golang "github.com/odvcencio/gotreesitter/grammars/go"
	"github.com/odvcencio/gotreesitter/grammars/javascript"
	"github.com/odvcencio/gotreesitter/grammars/python"
	"github.com/odvcencio/gotreesitter/grammars/rust"
	"github.com/odvcencio/gotreesitter/grammars/tsx"
	"github.com/odvcencio/gotreesitter/grammars/typescript"
)

type file struct {
	path string
	src  []byte
}

type gate struct {
	lang   *ts.Language
	engine string
	c      string
	parser *ts.Parser
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	langName := flag.String("lang", "go", "language")
	engine := flag.String("engine", "default", "default or old")
	list := flag.String("files", "", "file with one source path per line")
	c := flag.String("c", "", "path to the C harness built against tree-sitter")
	reps := flag.Int("reps", 0, "timing repetitions, 0 skips timing")
	sexp := flag.Bool("sexp", false, "also compare S-expressions file by file")
	incremental := flag.Bool("incremental", false, "also compare an incremental parse after an edit with a fresh parse")
	flat := flag.Bool("flat", false, "only compare the flat parse cat-grep matches over with the node tree, one flat reused across every file")
	flag.Parse()
	language, ok := map[string]func() *ts.Language{
		"go": golang.Language, "python": python.Language, "rust": rust.Language,
		"typescript": typescript.Language, "tsx": tsx.Language, "javascript": javascript.Language,
	}[*langName]
	if !ok {
		return fmt.Errorf("unknown -lang %q", *langName)
	}
	if *engine != "old" && *engine != "default" {
		return fmt.Errorf("unknown -engine %q", *engine)
	}
	if *c == "" || *list == "" {
		return errors.New("-c and -files are required")
	}
	if err := use(*engine); err != nil {
		return err
	}
	files, err := readFiles(*list)
	if err != nil {
		return err
	}
	g := &gate{lang: language(), engine: *engine, c: *c}
	g.parser = ts.NewParser(g.lang)
	if *flat {
		return g.compareFlat(files)
	}
	if err := g.compare(files, *sexp, *incremental); err != nil {
		return err
	}
	if *reps > 0 {
		return g.time(files, *reps)
	}
	return nil
}

func readFiles(list string) ([]file, error) {
	data, err := os.ReadFile(list)
	if err != nil {
		return nil, err
	}
	var files []file
	for _, path := range strings.Fields(string(data)) {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		files = append(files, file{path: path, src: src})
	}
	return files, nil
}

func use(engine string) error {
	if engine == "old" {
		return os.Setenv(ts.CCoreSwitchEnv, "0")
	}
	return os.Unsetenv(ts.CCoreSwitchEnv)
}

func (g *gate) declined(src []byte) bool {
	tree, ok := g.parser.ParseNewCore(src)
	if ok {
		tree.Release()
	}
	return !ok
}

func (g *gate) sexpOf(tree *ts.Tree) string {
	var b strings.Builder
	writeSexp(&b, tree.RootNode(), g.lang)
	return b.String()
}

func (g *gate) sameTree(a, b *ts.Tree) bool {
	return slices.Equal(nodeList(a.RootNode(), g.lang), nodeList(b.RootNode(), g.lang)) && g.sexpOf(a) == g.sexpOf(b)
}

func (g *gate) equalToOldCore(src []byte) (bool, error) {
	tree, err := g.parser.Parse(src)
	if err != nil {
		return false, err
	}
	defer tree.Release()
	if err := use("old"); err != nil {
		return false, err
	}
	old, err := g.parser.Parse(src)
	if err := errors.Join(err, use(g.engine)); err != nil {
		return false, err
	}
	defer old.Release()
	return g.sameTree(tree, old), nil
}

func (g *gate) incrementalEqualsFresh(f file) (bool, error) {
	at := len(f.src) / 2
	edited := slices.Concat(f.src[:at], []byte("zq"), f.src[at:])
	tree, err := g.parser.Parse(f.src)
	if err != nil {
		return false, err
	}
	defer tree.Release()
	row := uint32(bytes.Count(f.src[:at], []byte("\n")))
	start := ts.Point{Row: row, Column: uint32(at - bytes.LastIndexByte(f.src[:at], '\n') - 1)}
	tree.Edit(ts.InputEdit{StartByte: uint32(at), OldEndByte: uint32(at), NewEndByte: uint32(at + 2), StartPoint: start, OldEndPoint: start, NewEndPoint: ts.Point{Row: row, Column: start.Column + 2}})
	inc, err := g.parser.ParseIncremental(edited, tree)
	if err != nil {
		return false, err
	}
	defer inc.Release()
	fresh, err := g.parser.Parse(edited)
	if err != nil {
		return false, err
	}
	defer fresh.Release()
	return g.sameTree(inc, fresh), nil
}

func (g *gate) compare(files []file, sexp, incremental bool) error {
	want, err := g.cLists(files)
	if err != nil {
		return err
	}
	same, sexpSame, incSame := 0, 0, 0
	var declined []file
	var differ []string
	for _, f := range files {
		tree, err := g.parser.Parse(f.src)
		if err != nil {
			return fmt.Errorf("%s: %w", f.path, err)
		}
		switch {
		case g.engine == "default" && g.declined(f.src):
			declined = append(declined, f)
		case slices.Equal(nodeList(tree.RootNode(), g.lang), want[f.path]):
			same++
		default:
			differ = append(differ, f.path)
		}
		if sexp {
			c, err := g.cSexp(f.path)
			if err != nil {
				return err
			}
			if g.sexpOf(tree) == c {
				sexpSame++
			} else {
				fmt.Println("sexp differs from C:", f.path)
			}
		}
		tree.Release()
		if incremental {
			ok, err := g.incrementalEqualsFresh(f)
			if err != nil {
				return fmt.Errorf("%s: %w", f.path, err)
			}
			if ok {
				incSame++
			} else {
				fmt.Println("incremental differs from fresh:", f.path)
			}
		}
	}
	fmt.Printf("same %d of %d, declined %d\n", same, len(files), len(declined))
	cErrors, oldSame := 0, 0
	for _, f := range declined {
		c, err := g.cSexp(f.path)
		if err != nil {
			return err
		}
		if strings.Contains(c, "(ERROR") || strings.Contains(c, "(MISSING") {
			cErrors++
		} else {
			fmt.Println("declined, C clean:", f.path)
		}
		equal, err := g.equalToOldCore(f.src)
		if err != nil {
			return err
		}
		if equal {
			oldSame++
		} else {
			fmt.Println("declined, tree differs from the old core:", f.path)
		}
	}
	fmt.Printf("declined with ERROR or MISSING in C: %d of %d\n", cErrors, len(declined))
	fmt.Printf("declined with a tree equal to the old core: %d of %d\n", oldSame, len(declined))
	if incremental {
		fmt.Printf("incremental equal to fresh: %d of %d\n", incSame, len(files))
	}
	for _, path := range differ {
		fmt.Println("differs from C:", path)
	}
	if sexp {
		fmt.Printf("sexp same %d of %d\n", sexpSame, len(files))
	}
	return nil
}

func (g *gate) compareFlat(files []file) error {
	var flat []ts.FlatNode
	same, old := 0, 0
	for _, f := range files {
		tree, err := g.parser.Parse(f.src)
		if err != nil {
			return fmt.Errorf("%s: %w", f.path, err)
		}
		var fallback *ts.Tree
		if flat, fallback, err = g.parser.ParseFlat(f.src, flat); err != nil {
			return fmt.Errorf("%s: %w", f.path, err)
		}
		if fallback != nil {
			old++
			fallback.Release()
			tree.Release()
			continue
		}
		var lines []uint32
		for at, c := range f.src {
			if c == '\n' {
				lines = append(lines, uint32(at))
			}
		}
		if sameFlat(flat, 0, tree.RootNode(), g.lang, lines) {
			same++
		} else {
			fmt.Println("flat differs from the node tree:", f.path)
		}
		tree.Release()
	}
	fmt.Printf("flat same %d of %d, old core tree %d\n", same, len(files), old)
	return nil
}

func pointAt(newlines []uint32, at uint32) ts.Point {
	row := sort.Search(len(newlines), func(i int) bool { return newlines[i] >= at })
	if row == 0 {
		return ts.Point{Column: at}
	}
	return ts.Point{Row: uint32(row), Column: at - newlines[row-1] - 1}
}

func sameFlat(flat []ts.FlatNode, at uint32, n *ts.Node, lang *ts.Language, newlines []uint32) bool {
	got := &flat[at]
	if got.Start != n.StartByte() || got.End != n.EndByte() || n.StartPoint() != pointAt(newlines, got.Start) || n.EndPoint() != pointAt(newlines, got.End) || got.Symbol != n.Symbol() || int(got.KidCount) != n.ChildCount() ||
		got.Flags&ts.FlatNamed != 0 != n.IsNamed() || got.Flags&ts.FlatExtra != 0 != n.IsExtra() || n.IsMissing() {
		return false
	}
	for i := range n.ChildCount() {
		kid, child := got.Kids+uint32(i), n.Child(i)
		field := ""
		if id := flat[kid].Field; id != 0 && int(id) < len(lang.FieldNames) {
			field = lang.FieldNames[id]
		}
		if flat[kid].Parent != at || child.Parent() != n || field != n.FieldNameForChild(i, lang) || !sameFlat(flat, kid, child, lang, newlines) {
			return false
		}
	}
	return true
}

func nodeList(n *ts.Node, lang *ts.Language) []string {
	var out []string
	var walk func(*ts.Node)
	walk = func(n *ts.Node) {
		if n.IsNamed() && !n.IsError() && !n.IsMissing() {
			out = append(out, fmt.Sprintf("%s %d %d", n.Type(lang), n.StartByte(), n.EndByte()))
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(n)
	return out
}

func writeSexp(b *strings.Builder, n *ts.Node, lang *ts.Language) {
	if n.IsMissing() {
		b.WriteString("(MISSING " + n.Type(lang) + ")")
		return
	}
	b.WriteString("(" + n.Type(lang))
	writeSexpChildren(b, n, lang)
	b.WriteString(")")
}

func writeSexpChildren(b *strings.Builder, n *ts.Node, lang *ts.Language) {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if !child.IsNamed() && !child.IsMissing() {
			writeSexpChildren(b, child, lang)
			continue
		}
		b.WriteString(" ")
		if field := n.FieldNameForChild(i, lang); field != "" {
			b.WriteString(field + ": ")
		}
		writeSexp(b, child, lang)
	}
}

func (g *gate) cLists(files []file) (map[string][]string, error) {
	out, err := g.runC("-list", files)
	if err != nil {
		return nil, err
	}
	lists := map[string][]string{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		cols := strings.Split(scanner.Text(), "\t")
		if len(cols) != 4 {
			return nil, fmt.Errorf("unexpected C list line %q", scanner.Text())
		}
		lists[cols[0]] = append(lists[cols[0]], strings.Join(cols[1:], " "))
	}
	return lists, scanner.Err()
}

func (g *gate) runC(mode string, files []file) ([]byte, error) {
	var stdin strings.Builder
	for _, f := range files {
		stdin.WriteString(f.path + "\n")
	}
	cmd := exec.Command(g.c, mode)
	cmd.Stdin = strings.NewReader(stdin.String())
	return cmd.Output()
}

func (g *gate) cSexp(path string) (string, error) {
	out, err := exec.Command(g.c, "-sexp", path).Output()
	return strings.TrimSpace(string(out)), err
}

func (g *gate) time(files []file, reps int) error {
	for _, engine := range []string{"default", "old"} {
		if err := use(engine); err != nil {
			return err
		}
		var totals []float64
		for range reps {
			start := time.Now()
			for _, f := range files {
				tree, err := g.parser.Parse(f.src)
				if err != nil {
					return err
				}
				tree.Release()
			}
			totals = append(totals, float64(time.Since(start).Microseconds())/1000)
		}
		fmt.Printf("parse ms %s %.1f (median of %d)\n", engine, median(totals), reps)
	}
	var totals []float64
	for range reps {
		out, err := g.runC("-time", files)
		if err != nil {
			return err
		}
		var n int
		var ms float64
		if _, err := fmt.Sscanf(string(out), "files %d parse_ms %f", &n, &ms); err != nil {
			return fmt.Errorf("timing output of the C harness %q: %w", out, err)
		}
		totals = append(totals, ms)
	}
	fmt.Printf("parse ms c %.1f (median of %d)\n", median(totals), reps)
	return nil
}

func median(values []float64) float64 {
	slices.Sort(values)
	return values[len(values)/2]
}
