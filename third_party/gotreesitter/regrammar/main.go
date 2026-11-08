package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/gotreesitter/grammargen"
)

func main() {
	cli := flag.String("cli", "", "tree-sitter cli, an executable or its cli.js; empty rebuilds with grammargen")
	ts2go := flag.String("ts2go", "", "ts2go executable, required with -cli")
	flag.Parse()
	if (*cli == "") != (*ts2go == "") {
		fmt.Fprintln(os.Stderr, "regrammar: -cli and -ts2go go together")
		os.Exit(2)
	}
	for _, dir := range flag.Args() {
		started := time.Now()
		var blob []byte
		var err error
		if *cli == "" {
			blob, err = grammargenBlob(dir)
		} else {
			blob, err = cliBlob(dir, *cli, *ts2go)
		}
		out := filepath.Join(dir, "..", "..", "grammars", "grammar_blobs", filepath.Base(dir)+".bin")
		if err == nil {
			err = os.WriteFile(out, blob, 0o644)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "regrammar %s: %v\n", dir, err)
			os.Exit(1)
		}
		fmt.Printf("%s %d bytes in %s\n", out, len(blob), time.Since(started).Round(time.Millisecond))
	}
}

func grammargenBlob(dir string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(dir, "grammar.json"))
	if err != nil {
		return nil, err
	}
	grammar, err := grammargen.ImportGrammarJSON(data)
	if err != nil {
		return nil, err
	}
	_, blob, err := grammargen.GenerateLanguageAndBlob(grammar)
	return blob, err
}

func cliBlob(dir, cli, ts2go string) ([]byte, error) {
	work, err := os.MkdirTemp("", "regrammar")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	generate := []string{cli, "generate", filepath.Join(dir, "grammar.json"), "-o", work}
	if strings.HasSuffix(cli, ".js") {
		generate = append([]string{"node"}, generate...)
	}
	name := filepath.Base(dir)
	convert := []string{ts2go, "-input", filepath.Join(work, "parser.c"), "-output", filepath.Join(work, "x.go"), "-package", "grammars", "-name", name}
	for _, step := range [][]string{generate, convert} {
		if out, err := exec.Command(step[0], step[1:]...).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("%s: %w\n%s", filepath.Base(step[0]), err, out)
		}
	}
	return os.ReadFile(filepath.Join(work, "grammar_blobs", name+".bin"))
}
