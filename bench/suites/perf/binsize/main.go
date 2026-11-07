package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/tools/go/ast/astutil"

	"tofu/internal/tools/code/catgrep/lang"
)

const (
	mebibyte         = 1 << 20
	initRuns         = 7
	gotreesitterRoot = "github.com/odvcencio/gotreesitter"
	grammarsRoot     = gotreesitterRoot + "/grammars/"
	langPackage      = "./internal/tools/code/catgrep/lang"
	catgrepDir       = "internal/tools/code/catgrep"
	stubbedLang      = "package lang\n\ntype Lang uint8\n\nfunc (Lang) String() string { return \"\" }\n"
	stubbedSearch    = `package catgrep

import (
	"context"
	"errors"

	"tofu/internal/tools/code/catgrep/lang"
)

const Languages = ""

type Site struct {
	Start, End, Line int
	Before, After    string
}

type File struct {
	Path  string
	Sites []Site
}

func LangNamed(string) (lang.Lang, error) { return 0, errors.New("no cat-grep in this build") }

func Rewrite(context.Context, string, string, lang.Lang, []string) ([]File, error) {
	return nil, errors.New("no cat-grep in this build")
}
`
)

type grammar struct{ ImportPath, Name string }

type row struct {
	name                     string
	bytes                    int64
	initMillis, parserMillis float64
}

var initClock = regexp.MustCompile(`^init (\S+) @\S+ ms, ([0-9.]+) ms clock`)

func main() {
	fixtures := flag.String("fixtures", "", "a fresh copy of bench/suites/tools/fixtures; every source file under it is parsed")
	flag.Parse()
	if err := sizes(); err != nil {
		fmt.Fprintln(os.Stderr, "binsize:", err)
		os.Exit(1)
	}
	if *fixtures == "" {
		return
	}
	clean, err := check(*fixtures)
	if err != nil {
		fmt.Fprintln(os.Stderr, "binsize:", err)
		os.Exit(1)
	}
	patterns()
	if !clean {
		os.Exit(2)
	}
}

func sizes() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "binsize")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	grammars, err := listGrammars(root)
	if err != nil {
		return err
	}
	without, err := withoutCatgrep(root, scratch)
	if err != nil {
		return err
	}
	var rows []row
	tags := []string{"grammar_subset"}
	for i := -2; i <= len(grammars); i++ {
		name, overlay, tagged := "without cat-grep", without, ""
		switch {
		case i == len(grammars):
			name, overlay, tagged = "every language, built with -tags grammar_subset and one tag per language", map[string]string{}, strings.Join(tags, ",")
		case i >= -1:
			name = "cat-grep, no language"
			if i >= 0 {
				name = "+ " + path.Base(grammars[i].ImportPath)
				tags = append(tags, "grammar_subset_"+path.Base(grammars[i].ImportPath))
			}
			if overlay, err = langWithout(root, scratch, grammars[i+1:]); err != nil {
				return err
			}
		}
		r, err := build(root, scratch, fmt.Sprint(i+2), overlay, tagged)
		if err != nil {
			return err
		}
		r.name = name
		rows = append(rows, r)
		fmt.Fprintf(os.Stderr, "binsize: %s %d bytes, init %.2f ms, parser init %.2f ms\n", name, r.bytes, r.initMillis, r.parserMillis)
	}
	table := render(rows, strings.Join(tags, ","))
	fmt.Print(table)
	return os.WriteFile(filepath.Join(root, "bench", "suites", "perf", "binsize", "catgrep.md"), []byte(table), 0o644)
}

func listGrammars(root string) ([]grammar, error) {
	command := exec.Command("go", "list", "-deps", "-json=ImportPath,Name", langPackage)
	command.Dir, command.Stderr = root, os.Stderr
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	var grammars []grammar
	for decoder := json.NewDecoder(strings.NewReader(string(out))); decoder.More(); {
		var g grammar
		if err := decoder.Decode(&g); err != nil {
			return nil, err
		}
		rest, isGrammar := strings.CutPrefix(g.ImportPath, grammarsRoot)
		if isGrammar && !strings.Contains(rest, "/") && rest != "grammar_blobs" && rest != "runtime" {
			grammars = append(grammars, g)
		}
	}
	slices.SortFunc(grammars, func(a, b grammar) int { return strings.Compare(a.ImportPath, b.ImportPath) })
	return grammars, nil
}

func langWithout(root, scratch string, dropped []grammar) (map[string]string, error) {
	file := filepath.Join(root, catgrepDir, "lang", "lang.go")
	files := token.NewFileSet()
	parsed, err := parser.ParseFile(files, file, nil, 0)
	if err != nil {
		return nil, err
	}
	local := map[string]bool{}
	for _, spec := range slices.Clone(parsed.Imports) {
		path := strings.Trim(spec.Path.Value, `"`)
		at := slices.IndexFunc(dropped, func(g grammar) bool { return g.ImportPath == path })
		if at < 0 {
			continue
		}
		alias, written := dropped[at].Name, ""
		if spec.Name != nil {
			alias, written = spec.Name.Name, spec.Name.Name
		}
		local[alias] = true
		astutil.DeleteNamedImport(files, parsed, written, path)
	}
	astutil.Apply(parsed, func(c *astutil.Cursor) bool {
		if selector, ok := c.Node().(*ast.SelectorExpr); ok {
			if pkg, ok := selector.X.(*ast.Ident); ok && local[pkg.Name] {
				c.Replace(ast.NewIdent("nil"))
			}
		}
		return true
	}, nil)
	var source bytes.Buffer
	if err := printer.Fprint(&source, files, parsed); err != nil {
		return nil, err
	}
	subset := filepath.Join(scratch, fmt.Sprintf("lang-%d.go", len(dropped)))
	return map[string]string{file: subset}, os.WriteFile(subset, source.Bytes(), 0o644)
}

func withoutCatgrep(root, scratch string) (map[string]string, error) {
	overlay := map[string]string{}
	for name, source := range map[string]string{"lang_stub.go": stubbedLang, "catgrep_stub.go": stubbedSearch} {
		if err := os.WriteFile(filepath.Join(scratch, name), []byte(source), 0o644); err != nil {
			return nil, err
		}
	}
	langFiles, err := filepath.Glob(filepath.Join(root, catgrepDir, "lang", "*.go"))
	if err != nil {
		return nil, err
	}
	for _, file := range langFiles {
		overlay[file] = ""
	}
	overlay[langFiles[0]] = filepath.Join(scratch, "lang_stub.go")
	overlay[filepath.Join(root, catgrepDir, "catgrep.go")] = filepath.Join(scratch, "catgrep_stub.go")
	return overlay, nil
}

func build(root, scratch, name string, replace map[string]string, tags string) (row, error) {
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": replace})
	if err != nil {
		return row{}, err
	}
	overlayFile := filepath.Join(scratch, "overlay-"+name+".json")
	if err := os.WriteFile(overlayFile, overlay, 0o644); err != nil {
		return row{}, err
	}
	binary := filepath.Join(scratch, "tofu-"+name+".exe")
	command := exec.Command("go", "build", "-overlay", overlayFile, "-tags", tags, "-o", binary, "./apps/cli")
	command.Dir, command.Stdout, command.Stderr = root, os.Stderr, os.Stderr
	if err := command.Run(); err != nil {
		return row{}, fmt.Errorf("go build %s: %w", name, err)
	}
	defer func() { _ = os.Remove(binary) }()
	info, err := os.Stat(binary)
	if err != nil {
		return row{}, err
	}
	total, parser, err := initMillis(binary)
	return row{bytes: info.Size(), initMillis: total, parserMillis: parser}, err
}

func initMillis(binary string) (float64, float64, error) {
	total, parser := make([]float64, initRuns), make([]float64, initRuns)
	for i := range total {
		command := exec.Command(binary, "version")
		command.Env = append(os.Environ(), "GODEBUG=inittrace=1")
		var trace strings.Builder
		command.Stderr = &trace
		if err := command.Run(); err != nil {
			return 0, 0, fmt.Errorf("%s version: %w", binary, err)
		}
		for _, line := range strings.Split(trace.String(), "\n") {
			match := initClock.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			clock, err := strconv.ParseFloat(match[2], 64)
			if err != nil {
				return 0, 0, err
			}
			total[i] += clock
			if strings.HasPrefix(match[1], gotreesitterRoot) || strings.HasPrefix(match[1], "tofu/"+catgrepDir) {
				parser[i] += clock
			}
		}
	}
	slices.Sort(total)
	slices.Sort(parser)
	return total[initRuns/2], parser[initRuns/2], nil
}

func render(rows []row, tags string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# tofu binary size and start time with and without cat-grep\n\nWritten by `go run ./bench/suites/perf/binsize` on %s, %s, %s/%s. Each language row is a build with that grammar added to every row above it. "+
		"init is the sum of every package init clock in `GODEBUG=inittrace=1 tofu version`, and parser init the part spent in the parser and cat-grep packages, each the median of %d runs.\n\n",
		time.Now().Format(time.DateOnly), runtime.Version(), runtime.GOOS, runtime.GOARCH, initRuns)
	out.WriteString("| build | bytes | MiB | bytes added | init ms | parser init ms |\n|---|---:|---:|---:|---:|---:|\n")
	for i, r := range rows {
		added := ""
		if i > 0 {
			added = fmt.Sprint(r.bytes - rows[i-1].bytes)
		}
		fmt.Fprintf(&out, "| %s | %d | %.2f | %s | %.2f | %.2f |\n", r.name, r.bytes, float64(r.bytes)/mebibyte, added, r.initMillis, r.parserMillis)
	}
	every, tagged := rows[len(rows)-2], rows[len(rows)-1]
	fmt.Fprintf(&out, "\nWith cat-grep and every language (%d) the binary is %d bytes, %.2f MiB, %d bytes more than without cat-grep, and the parser packages take %.2f ms of init. "+
		"Built with the tags below, which drop the scanners of every grammar tofu does not use, it is %d bytes and %.2f ms.\n\n```\n-tags %s\n```\n",
		len(rows)-3, every.bytes, float64(every.bytes)/mebibyte, every.bytes-rows[0].bytes, every.parserMillis, tagged.bytes, tagged.parserMillis, tags)
	return out.String()
}

func check(fixtures string) (bool, error) {
	clean := true
	err := filepath.WalkDir(fixtures, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		l, ok := lang.FromPath(file)
		if !ok {
			return nil
		}
		source, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(fixtures, file)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		verdict, faulted, err := parse(source, l)
		if err != nil {
			return err
		}
		fmt.Printf("%-10s %-32s %s\n", l, relative, verdict)
		if faulted && !slices.Contains(strings.Split(relative, "/"), "broken") {
			clean = false
		}
		return nil
	})
	return clean, err
}

func patterns() {
	fmt.Println()
	cases := []struct {
		l       lang.Lang
		pattern string
	}{
		{lang.Go, "$A = 0"},
		{lang.Go, "func $A($$$) $B { $$$ }"},
		{lang.TypeScript, "$A = 0"},
		{lang.Tsx, "$A = 0"},
		{lang.Python, "$A = 0"},
		{lang.Python, "print($$$)"},
		{lang.Rust, "Some($A)"},
		{lang.Rust, "let ($X, $Y) = $$$T;"},
	}
	for _, c := range cases {
		prepared := c.l.PreprocessPattern(c.pattern)
		verdict, _, err := parse([]byte(prepared), c.l)
		if err != nil {
			verdict = err.Error()
		}
		fmt.Printf("%-10s %-28s %s\n", c.l, prepared, verdict)
	}
}

func parse(source []byte, l lang.Lang) (string, bool, error) {
	tree, err := lang.Parse(source, l)
	if err != nil {
		return "", false, err
	}
	defer tree.Release()
	fault, faulted := lang.FirstFault(tree)
	switch {
	case !faulted:
		return "no error node", false, nil
	case fault.Missing:
		return fmt.Sprintf("missing %s at %d:%d", fault.Kind, fault.At.Row+1, fault.At.Column+1), true, nil
	}
	return fmt.Sprintf("error node at %d:%d", fault.At.Row+1, fault.At.Column+1), true, nil
}
