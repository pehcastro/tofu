package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tofu/internal/tools/code/catgrep"
	"tofu/internal/tools/code/catgrep/lang"
)

const cleanFloorPercent = 98

type search struct {
	Lang    string `json:"lang"`
	Pattern string `json:"pattern"`
}

type language struct {
	search
	Scripts []search `json:"scripts"`
	Repos   []string `json:"repos"`
}

type parsed struct {
	file, fault string
	took        time.Duration
}

func main() {
	config := flag.String("config", filepath.Join("bench", "suites", "tools", "catgrep", "repos", "repos.json"), "languages, one pattern each and the repositories to search")
	dir := flag.String("dir", "", "where the repositories are cloned, outside this repository")
	only := flag.String("only", "", "languages from the config, comma separated")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "repos: -dir is required")
		os.Exit(2)
	}
	languages, err := load(*config, *only)
	if err == nil {
		err = clone(languages, *dir)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "repos:", err)
		os.Exit(1)
	}
	if !report(languages, *dir) {
		os.Exit(2)
	}
}

func load(config, only string) ([]language, error) {
	data, err := os.ReadFile(config)
	if err != nil {
		return nil, err
	}
	var all []language
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("%s: %w", config, err)
	}
	var picked []language
	for _, l := range all {
		if only == "" || slices.Contains(strings.Split(only, ","), l.Lang) {
			picked = append(picked, l)
		}
	}
	return picked, nil
}

func checkout(dir, url string) string {
	return filepath.Join(dir, path.Base(path.Dir(url))+"__"+path.Base(url))
}

func clone(languages []language, dir string) error {
	var urls []string
	for _, l := range languages {
		for _, url := range l.Repos {
			if _, err := os.Stat(checkout(dir, url)); err != nil && !slices.Contains(urls, url) {
				urls = append(urls, url)
			}
		}
	}
	failures := make([]error, len(urls))
	parallel(len(urls), func(i int) {
		output, err := exec.Command("git", "clone", "--depth", "1", "--quiet", urls[i], checkout(dir, urls[i])).CombinedOutput()
		if err != nil {
			failures[i] = fmt.Errorf("clone %s: %w: %s", urls[i], err, output)
		}
	})
	return errors.Join(failures...)
}

func report(languages []language, dir string) bool {
	fmt.Printf("%-11s %5s %6s %6s %7s %7s %9s  %s\n", "lang", "repos", "files", "error", "clean%", "matches", "slowest", "pattern")
	var exceptions, scripts []string
	passed := true
	for _, entry := range languages {
		l, ok := lang.Named(entry.Lang)
		if !ok {
			fmt.Printf("%-11s not a language cat-grep parses\n", entry.Lang)
			passed = false
			continue
		}
		var files []parsed
		faults, matches, slowest := 0, 0, parsed{}
		for _, url := range entry.Repos {
			root := checkout(dir, url)
			repoFiles, err := parseAll(l, root, dir)
			if err == nil {
				var found []catgrep.File
				found, err = catgrep.Rewrite(context.Background(), entry.Pattern, "", l, []string{root})
				for _, f := range found {
					matches += len(f.Sites)
				}
			}
			if err != nil {
				exceptions = append(exceptions, fmt.Sprintf("%-11s %s: %v", l, path.Base(url), err))
				passed = false
			}
			files = append(files, repoFiles...)
			for _, s := range entry.Scripts {
				scripts = append(scripts, scriptRow(l, s, root, dir))
			}
		}
		for _, f := range files {
			if f.took > slowest.took {
				slowest = f
			}
			if f.fault != "" {
				faults++
				exceptions = append(exceptions, fmt.Sprintf("%-11s %s %s", l, f.file, f.fault))
			}
		}
		clean := 100.0
		if len(files) > 0 {
			clean = 100 * float64(len(files)-faults) / float64(len(files))
		}
		passed = passed && len(files) > 0 && clean >= cleanFloorPercent
		fmt.Printf("%-11s %5d %6d %6d %7.2f %7d %7dms  %s   slowest %s\n", l, len(entry.Repos), len(files), faults, clean, matches, slowest.took.Milliseconds(), entry.Pattern, slowest.file)
	}
	fmt.Printf("\nscript and style code inside host files\n")
	for _, s := range scripts {
		fmt.Println(s)
	}
	fmt.Printf("\nexceptions: %d\n", len(exceptions))
	for _, e := range exceptions {
		fmt.Println(e)
	}
	return passed
}

func scriptRow(host lang.Lang, s search, root, dir string) string {
	inner, ok := lang.Named(s.Lang)
	if !ok {
		return fmt.Sprintf("%s > %s: not a language cat-grep parses", host, s.Lang)
	}
	found, err := catgrep.Rewrite(context.Background(), s.Pattern, "", inner, []string{root})
	if err != nil {
		return fmt.Sprintf("%s > %s %q: %v", host, inner, s.Pattern, err)
	}
	matches, example := 0, "none"
	for _, f := range found {
		if l, _ := lang.FromPath(f.Path); l != host {
			continue
		}
		if matches == 0 {
			example = fmt.Sprintf("%s:%d %s", relative(dir, f.Path), f.Sites[0].Line, strings.Fields(f.Sites[0].Before)[0])
		}
		matches += len(f.Sites)
	}
	return fmt.Sprintf("%-6s > %-10s %-16q matches in %s files %5d, first %s", host, inner, s.Pattern, host, matches, example)
}

func parseAll(l lang.Lang, root, dir string) ([]parsed, error) {
	candidates, err := catgrep.Files(l, []string{root})
	if err != nil {
		return nil, err
	}
	var files []parsed
	for _, f := range candidates {
		if own, _ := lang.FromPath(f); own == l {
			files = append(files, parsed{file: f})
		}
	}
	failures := make([]error, len(files))
	parallel(len(files), func(i int) {
		started := time.Now()
		files[i].fault, failures[i] = faultOf(files[i].file, l)
		files[i].took, files[i].file = time.Since(started), relative(dir, files[i].file)
	})
	return files, errors.Join(failures...)
}

func faultOf(file string, l lang.Lang) (string, error) {
	source, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	tree, err := lang.Parse(source, l)
	if err != nil {
		return "", err
	}
	defer tree.Release()
	f, faulted := lang.FirstFault(tree)
	switch {
	case tree.ParseStoppedEarly():
		return fmt.Sprintf("parse stopped early: %s", tree.ParseStopReason()), nil
	case !faulted:
		return "", nil
	case f.Missing:
		return fmt.Sprintf("%d:%d missing %s", f.At.Row+1, f.At.Column+1, f.Kind), nil
	}
	return fmt.Sprintf("%d:%d error node", f.At.Row+1, f.At.Column+1), nil
}

func relative(base, file string) string {
	rel, err := filepath.Rel(base, file)
	if err != nil {
		return file
	}
	return filepath.ToSlash(rel)
}

func parallel(n int, do func(int)) {
	var next atomic.Int64
	var workers sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		workers.Go(func() {
			for i := int(next.Add(1) - 1); i < n; i = int(next.Add(1) - 1) {
				do(i)
			}
		})
	}
	workers.Wait()
}
