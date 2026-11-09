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
	"strings"
	"time"

	ts "github.com/odvcencio/gotreesitter"
	golang "github.com/odvcencio/gotreesitter/grammars/go"
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
	engine := flag.String("engine", "new", "old or new")
	list := flag.String("files", "", "file with one source path per line")
	c := flag.String("c", "", "path to the C harness built against tree-sitter")
	reps := flag.Int("reps", 0, "timing repetitions, 0 skips timing")
	sexp := flag.Bool("sexp", false, "also compare S-expressions file by file")
	flag.Parse()
	if *langName != "go" {
		return fmt.Errorf("unknown -lang %q", *langName)
	}
	if *engine != "old" && *engine != "new" {
		return fmt.Errorf("unknown -engine %q", *engine)
	}
	if *c == "" || *list == "" {
		return errors.New("-c and -files are required")
	}
	if err := os.Unsetenv(ts.CCoreSwitchEnv); err != nil {
		return err
	}
	files, err := readFiles(*list)
	if err != nil {
		return err
	}
	g := &gate{lang: golang.Language(), engine: *engine, c: *c}
	g.parser = ts.NewParser(g.lang)
	if err := g.compare(files, *sexp); err != nil {
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

func (g *gate) parse(src []byte, engine string) (*ts.Tree, bool, error) {
	if engine == "new" {
		if tree, ok := g.parser.ParseNewCore(src); ok {
			return tree, false, nil
		}
		tree, err := g.parser.Parse(src)
		return tree, true, err
	}
	tree, err := g.parser.Parse(src)
	return tree, false, err
}

func (g *gate) compare(files []file, sexp bool) error {
	want, err := g.cLists(files)
	if err != nil {
		return err
	}
	same, sexpSame := 0, 0
	var declined, differ []string
	for _, f := range files {
		tree, wasDeclined, err := g.parse(f.src, g.engine)
		if err != nil {
			return fmt.Errorf("%s: %w", f.path, err)
		}
		got := nodeList(tree.RootNode(), g.lang)
		switch {
		case wasDeclined:
			declined = append(declined, f.path)
		case slices.Equal(got, want[f.path]):
			same++
		default:
			differ = append(differ, f.path)
		}
		if sexp {
			c, err := g.cSexp(f.path)
			if err != nil {
				return err
			}
			var b strings.Builder
			writeSexp(&b, tree.RootNode(), g.lang)
			if b.String() == c {
				sexpSame++
			} else {
				fmt.Println("sexp differs from C:", f.path)
			}
		}
		tree.Release()
	}
	fmt.Printf("same %d of %d, declined %d\n", same, len(files), len(declined))
	cErrors := 0
	for _, path := range declined {
		c, err := g.cSexp(path)
		if err != nil {
			return err
		}
		if strings.Contains(c, "(ERROR") || strings.Contains(c, "(MISSING") {
			cErrors++
		} else {
			fmt.Println("declined, C clean:", path)
		}
	}
	fmt.Printf("declined with ERROR or MISSING in C: %d of %d\n", cErrors, len(declined))
	for _, path := range differ {
		fmt.Println("differs from C:", path)
	}
	if sexp {
		fmt.Printf("sexp same %d of %d\n", sexpSame, len(files))
	}
	return nil
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
	for _, engine := range []string{"new", "old"} {
		var totals []float64
		for range reps {
			start := time.Now()
			for _, f := range files {
				tree, _, err := g.parse(f.src, engine)
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
