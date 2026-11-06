package codemod

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	platformfs "tofu/internal/platform/fs"
	"tofu/internal/tools/code/typecheck/checkers"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit/patch"
)

const astGrepInstall = "npm install --global @ast-grep/cli, cargo install ast-grep --locked, pip install ast-grep-cli, or brew install ast-grep on macOS"

type site struct {
	start, end int
	line       int
	before     string
	after      string
}

type fileSites struct {
	path  string
	sites []site
}

type change struct {
	path  string
	after []byte
}

type arguments struct {
	Pattern string   `json:"pattern"`
	Rewrite string   `json:"rewrite"`
	Lang    string   `json:"lang"`
	Paths   []string `json:"paths"`
	Apply   bool     `json:"apply"`
	Expect  *int     `json:"expect"`
}

type Codemod struct{ root contract.Root }

func New(dir string) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return Codemod{root: root}, err
}

func (Codemod) Name() string { return "codemod" }

func (Codemod) Definition() core.Tool {
	return core.Tool{
		Name: "codemod",
		Description: "rewrites one code shape everywhere it appears, matched on the syntax tree rather than the text, so a string or a comment that looks the same is never touched. " +
			"use it instead of edit with replace_all, or one edit per site, when the same change repeats. " +
			"pattern and rewrite are ast-grep patterns: $NAME matches one node, $$$NAME any run of nodes such as call arguments or statements, $_ and $$$ match without capturing, " +
			"and a name used twice must match the same code. every name in rewrite must be captured in pattern. lang is the language, go, ts, tsx, js, rust, python and others. " +
			"paths are files or directories under the working directory, all of it when left out. " +
			"the first call previews: every match with its line and a diff, nothing written. apply true writes those same sites, " +
			"refuses when expect is given and the match count differs from it, and returns the checker's errors after the write. " +
			"go works without anything installed; every other language needs ast-grep on PATH",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string"},
				"rewrite": map[string]any{"type": "string"},
				"lang":    map[string]any{"type": "string"},
				"paths":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"apply":   map[string]any{"type": "boolean"},
				"expect":  map[string]any{"type": "integer"},
			},
			"required": []string{"pattern", "rewrite", "lang"},
		},
	}
}

func (c Codemod) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args arguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("codemod: arguments are not the expected shape: %w", err)
	}
	args.Lang = strings.ToLower(strings.TrimSpace(args.Lang))
	if args.Lang == "golang" {
		args.Lang = "go"
	}
	if strings.TrimSpace(args.Pattern) == "" || args.Lang == "" {
		return contract.Failure("codemod: pattern and lang are required"), nil
	}
	paths := []string{"."}
	if len(args.Paths) > 0 {
		paths = args.Paths
	}
	for i, path := range paths {
		resolved, err := c.root.Resolve(path)
		if err != nil {
			return contract.Failure("codemod: " + err.Error()), nil
		}
		paths[i] = resolved
	}
	ctx, cancel := context.WithTimeout(ctx, konst.CodemodRunMillis*time.Millisecond)
	defer cancel()
	var (
		found  []fileSites
		engine string
		err    error
	)
	switch binary, lookErr := exec.LookPath("ast-grep"); {
	case lookErr == nil:
		engine = "ast-grep"
		found, err = astGrepSites(ctx, binary, string(c.root), args, paths)
	case args.Lang == "go":
		var unparsed []string
		found, unparsed, err = goSites(args.Pattern, args.Rewrite, paths)
		engine = "go/ast, as ast-grep is not on PATH"
		for i, path := range unparsed {
			relative, _ := filepath.Rel(string(c.root), path)
			unparsed[i] = filepath.ToSlash(relative)
		}
		if len(unparsed) > 0 {
			engine += ", skipping what does not parse: " + strings.Join(unparsed, ", ")
		}
	default:
		return contract.Failure("codemod: ast-grep is not on PATH, and lang " + args.Lang + " needs it. install it with " + astGrepInstall + ", then call codemod again. lang go works without it"), nil
	}
	if err != nil {
		return contract.Failure("codemod: " + err.Error()), nil
	}
	return c.report(ctx, args, engine, found)
}

func (c Codemod) report(ctx context.Context, args arguments, engine string, found []fileSites) (contract.Result, error) {
	matches := 0
	for _, file := range found {
		matches += len(file.sites)
	}
	if matches == 0 {
		return contract.Result{Content: fmt.Sprintf("codemod: 0 matches for %s in lang %s, through %s. nothing written", args.Pattern, args.Lang, engine)}, nil
	}
	if args.Apply && args.Expect != nil && *args.Expect != matches {
		return contract.Failure(fmt.Sprintf("codemod: expected %d matches and found %d, so nothing was written. preview again and apply with the new count", *args.Expect, matches)), nil
	}
	var listing, diffs strings.Builder
	shown := 0
	changes := make([]change, 0, len(found))
	for _, file := range found {
		relative, _ := filepath.Rel(string(c.root), file.path)
		relative = filepath.ToSlash(relative)
		for _, s := range file.sites {
			if shown++; shown <= konst.CodemodSitesShown {
				fmt.Fprintf(&listing, "%s:%d %s -> %s\n", relative, s.line, firstLine(s.before), firstLine(s.after))
			}
		}
		before, err := os.ReadFile(file.path)
		if err != nil {
			return contract.Result{}, fmt.Errorf("codemod: %w", err)
		}
		after, err := rewritten(file.path, before, file.sites)
		if err != nil {
			return contract.Failure("codemod: " + err.Error() + ". nothing written"), nil
		}
		if diffs.Len() < konst.CodemodDiffBytes {
			diffs.WriteString(patch.Unified(relative, string(before), string(after)))
		}
		changes = append(changes, change{path: file.path, after: after})
	}
	if shown > konst.CodemodSitesShown {
		fmt.Fprintf(&listing, "%d more matches not listed\n", shown-konst.CodemodSitesShown)
	}
	diff := diffs.String()
	if len(diff) > konst.CodemodDiffBytes {
		diff = diff[:konst.CodemodDiffBytes] + "\n... diff cut at " + fmt.Sprint(konst.CodemodDiffBytes) + " bytes\n"
	}
	head := fmt.Sprintf("codemod: %s in %s, through %s", counted(matches, "match", "matches"), counted(len(found), "file", "files"), engine)
	if !args.Apply {
		apply := fmt.Sprintf("to write these sites: the same call with \"apply\": true, \"expect\": %d\n", matches)
		return contract.Result{Content: head + ". preview, nothing written\n" + listing.String() + diff + apply}, nil
	}
	for _, file := range changes {
		info, err := os.Stat(file.path)
		if err != nil {
			return contract.Result{}, fmt.Errorf("codemod: %w", err)
		}
		if err := platformfs.WriteFile(file.path, file.after, info.Mode().Perm()); err != nil {
			return contract.Result{}, fmt.Errorf("codemod: wrote %s: %w", file.path, err)
		}
	}
	return contract.Result{Content: head + ". applied\n" + listing.String() + diff + c.checked(ctx, args.Lang, changes)}, nil
}

func rewritten(path string, before []byte, sites []site) ([]byte, error) {
	var after bytes.Buffer
	at := 0
	for _, s := range sites {
		if s.start < at || s.end > len(before) {
			return nil, fmt.Errorf("two matches overlap in %s, near line %d: narrow the pattern", path, s.line)
		}
		after.Write(before[at:s.start])
		after.WriteString(s.after)
		at = s.end
	}
	after.Write(before[at:])
	if filepath.Ext(path) != ".go" {
		return after.Bytes(), nil
	}
	return goFormatted(path, before, after.Bytes())
}

func (c Codemod) checked(ctx context.Context, lang string, changes []change) string {
	switch lang {
	case "go":
		packages := []string{}
		for _, file := range changes {
			relative, _ := filepath.Rel(string(c.root), filepath.Dir(file.path))
			pkg := "./" + filepath.ToSlash(relative)
			if relative == "." {
				pkg = "."
			}
			if !slices.Contains(packages, pkg) {
				packages = append(packages, pkg)
			}
		}
		slices.Sort(packages)
		command := "go vet " + strings.Join(packages, " ")
		vet := exec.CommandContext(ctx, "go", append([]string{"vet"}, packages...)...)
		vet.Dir, vet.WaitDelay = string(c.root), konst.CodemodRunMillis*time.Millisecond
		out, err := vet.CombinedOutput()
		var exit *exec.ExitError
		switch {
		case err == nil:
			return "checker: " + command + ": no errors\n"
		case errors.As(err, &exit):
			return "checker: " + command + " found errors:\n" + clipped(string(out))
		}
		return "checker: " + command + " did not run: " + err.Error() + "\n"
	case "ts", "tsx", "typescript":
		var cold *checkers.Typecheckers
		result, err := cold.Check(ctx, string(c.root))
		if err != nil {
			return "checker: typecheck did not run: " + err.Error() + "\n"
		}
		return "checker: " + clipped(result.Content)
	}
	return "checker: none for lang " + lang + ". run its build or tests to check the change\n"
}

func clipped(text string) string {
	if len(text) > konst.CodemodCheckerBytes {
		text = text[:konst.CodemodCheckerBytes] + "\n... cut at " + fmt.Sprint(konst.CodemodCheckerBytes) + " bytes"
	}
	return strings.TrimRight(text, "\n") + "\n"
}

func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprint(n, " ", many)
}

func firstLine(text string) string {
	line, _, cut := strings.Cut(strings.TrimSpace(text), "\n")
	if cut {
		return line + " ..."
	}
	return line
}
