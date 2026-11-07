package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/tools/code/catgrep/lang"
)

const mebibyte = 1 << 20

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
	grammars := []string{
		"github.com/odvcencio/gotreesitter/grammars/go",
		"github.com/odvcencio/gotreesitter/grammars/typescript",
		"github.com/odvcencio/gotreesitter/grammars/tsx",
		"github.com/odvcencio/gotreesitter/grammars/python",
		"github.com/odvcencio/gotreesitter/grammars/rust",
	}
	fmt.Printf("%-12s %12s %8s %10s\n", "build", "bytes", "MiB", "added")
	var before, previous int64
	for i := 0; i <= len(grammars); i++ {
		size, err := build(root, scratch, grammars[:i])
		if err != nil {
			return err
		}
		name := "before"
		if i == 0 {
			before, previous = size, size
		} else {
			name = "+" + path.Base(grammars[i-1])
		}
		fmt.Printf("%-12s %12d %8.2f %10d\n", name, size, float64(size)/mebibyte, size-previous)
		previous = size
	}
	fmt.Printf("total added  %d bytes, %.2f MiB\n\n", previous-before, float64(previous-before)/mebibyte)
	return nil
}

func build(root, scratch string, grammars []string) (int64, error) {
	source := "package main\n"
	for _, pkg := range grammars {
		source += fmt.Sprintf("import _ %q\n", pkg)
	}
	importer := filepath.Join(scratch, fmt.Sprintf("grammars%d.go", len(grammars)))
	if err := os.WriteFile(importer, []byte(source), 0o644); err != nil {
		return 0, err
	}
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": {filepath.Join(root, "apps", "cli", "zz_binsize_grammars.go"): importer}})
	if err != nil {
		return 0, err
	}
	overlayFile := filepath.Join(scratch, "overlay.json")
	if err := os.WriteFile(overlayFile, overlay, 0o644); err != nil {
		return 0, err
	}
	binary := filepath.Join(scratch, fmt.Sprintf("tofu%d.exe", len(grammars)))
	command := exec.Command("go", "build", "-overlay", overlayFile, "-o", binary, "./apps/cli")
	command.Dir, command.Stdout, command.Stderr = root, os.Stderr, os.Stderr
	if err := command.Run(); err != nil {
		return 0, fmt.Errorf("go build with %d grammars: %w", len(grammars), err)
	}
	info, err := os.Stat(binary)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
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
