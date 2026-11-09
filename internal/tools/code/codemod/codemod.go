package codemod

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	platformfs "tofu/internal/platform/fs"
	"tofu/internal/tools/code/catgrep"
	"tofu/internal/tools/code/catgrep/lang"
	"tofu/internal/tools/code/catgrep/tree"
	"tofu/internal/tools/code/typecheck/checkers"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit/patch"
)

type change struct {
	path, relative string
	before, after  []byte
}

type arguments struct {
	Pattern string          `json:"pattern"`
	Rule    json.RawMessage `json:"rule"`
	Rewrite string          `json:"rewrite"`
	Lang    string          `json:"lang"`
	Paths   []string        `json:"paths"`
	Apply   bool            `json:"apply"`
	Expect  *int            `json:"expect"`
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
			"pattern and rewrite are code in lang with meta variables: $NAME matches one node, $$$NAME any run of nodes such as call arguments or statements, $_ and $$$ match without capturing, " +
			"and a name used twice must match the same code. every name in rewrite must be captured in pattern. lang is one of " + catgrep.Languages + ". " +
			"paths are files or directories under the working directory, all of it when left out. " +
			"the first call previews: every match with its line and a diff, nothing written. apply true writes those same sites, " +
			"refuses when expect is given and the match count differs from it, and returns the checker's errors after the write. " +
			"give rule instead of pattern to match on more than one condition: an object whose keys are pattern, kind (a node kind of lang), regex (over the node text), " +
			"all and any (lists of rules), not (a rule), inside and has (a rule plus stopBy: neighbor, the default, end, or a rule to stop at, and field: a field name of lang). " +
			"the matched node is replaced whole by rewrite. a pattern never matches the text of a string or a comment, and a name in an f-string or template interpolation is code, so a plain rename needs no rule. " +
			"to rename old only within the function or call that names main, where has looks at each enclosing node's own children: " +
			"{\"pattern\": \"old\", \"inside\": {\"stopBy\": \"end\", \"has\": {\"regex\": \"^main$\"}}}",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string"},
				"rule":    map[string]any{"type": "object"},
				"rewrite": map[string]any{"type": "string"},
				"lang":    map[string]any{"type": "string"},
				"paths":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"apply":   map[string]any{"type": "boolean"},
				"expect":  map[string]any{"type": "integer"},
			},
			"required": []string{"rewrite", "lang"},
		},
	}
}

func (c Codemod) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args arguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("codemod: arguments are not the expected shape: %w", err)
	}
	hasPattern, hasRule := strings.TrimSpace(args.Pattern) != "", len(args.Rule) > 0
	if hasPattern == hasRule || strings.TrimSpace(args.Lang) == "" {
		return contract.Failure("codemod: lang and one of pattern or rule are required"), nil
	}
	l, err := catgrep.LangNamed(args.Lang)
	if err != nil {
		return contract.Failure("codemod: " + err.Error()), nil
	}
	args.Lang = l.String()
	paths := []string{"."}
	if len(args.Paths) > 0 {
		paths = args.Paths
	}
	command := cmp.Or(strings.TrimSpace(args.Pattern), "rule") + " under " + strings.Join(paths, " ")
	for i, path := range paths {
		resolved, err := c.root.Resolve(path)
		if err != nil {
			return contract.Failure("codemod: " + err.Error()), nil
		}
		paths[i] = resolved
	}
	ctx, cancel := context.WithTimeout(ctx, konst.CodemodRunMillis*time.Millisecond)
	defer cancel()
	var found []catgrep.File
	if hasRule {
		found, err = catgrep.RewriteRule(ctx, args.Rule, args.Rewrite, l, paths)
	} else {
		found, err = catgrep.Rewrite(ctx, args.Pattern, args.Rewrite, l, paths)
	}
	if err != nil {
		return contract.Failure("codemod: " + err.Error()), nil
	}
	for i, file := range found {
		captures, err := formatCaptures(l, file)
		if err != nil {
			return contract.Failure("codemod: " + file.Path + ": " + err.Error()), nil
		}
		found[i].Sites = slices.SortedStableFunc(slices.Values(append(file.Sites, captures...)), func(a, b catgrep.Site) int { return a.Start - b.Start })
	}
	result, err := c.report(ctx, args, found)
	result.Command = command
	return result, err
}

func (c Codemod) report(ctx context.Context, args arguments, found []catgrep.File) (contract.Result, error) {
	matches := 0
	for _, file := range found {
		matches += len(file.Sites)
	}
	if matches == 0 {
		searched := args.Pattern
		if len(args.Rule) > 0 {
			searched = "rule " + string(args.Rule)
		}
		return contract.Result{Content: fmt.Sprintf("codemod: 0 matches for %s in lang %s. nothing written", searched, args.Lang)}, nil
	}
	if args.Apply && args.Expect != nil && *args.Expect != matches {
		return contract.Failure(fmt.Sprintf("codemod: expected %d matches and found %d, so nothing was written. preview again and apply with the new count", *args.Expect, matches)), nil
	}
	if err := unparsable(args.Lang, found); err != nil {
		return contract.Failure("codemod: " + err.Error() + ". nothing written"), nil
	}
	var listing, diffs strings.Builder
	shown := 0
	for _, file := range found {
		for _, s := range file.Sites {
			if shown++; shown <= konst.CodemodSitesShown {
				fmt.Fprintf(&listing, "%s:%d %s -> %s\n", c.relative(file.Path), s.Line, firstLine(s.Before), firstLine(s.After))
			}
		}
	}
	if shown > konst.CodemodSitesShown {
		fmt.Fprintf(&listing, "%d more matches not listed\n", shown-konst.CodemodSitesShown)
	}
	head := fmt.Sprintf("codemod: %s in %s", counted(matches, "match", "matches"), counted(len(found), "file", "files"))
	if !args.Apply {
		for _, file := range found {
			if diffs.Len() >= konst.CodemodDiffBytes {
				break
			}
			before, err := os.ReadFile(file.Path)
			if err != nil {
				return contract.Failure("codemod: " + err.Error() + ". nothing written"), nil
			}
			diffs.WriteString(siteDiff(c.relative(file.Path), before, file.Sites))
		}
		apply := fmt.Sprintf("to write these sites: the same call with \"apply\": true, \"expect\": %d\n", matches)
		return contract.Result{Content: head + ". preview, nothing written\n" + listing.String() + clippedDiff(diffs.String()) + apply}, nil
	}
	changes, spliced := make([]change, len(found)), make([][]byte, len(found))
	for i, file := range found {
		before, err := os.ReadFile(file.Path)
		if err != nil {
			return contract.Failure("codemod: " + err.Error() + ". nothing written"), nil
		}
		var after bytes.Buffer
		splice(&after, before, 0, len(before), applied(file.Sites))
		changes[i] = change{path: file.Path, relative: c.relative(file.Path), before: before, after: after.Bytes()}
		spliced[i] = changes[i].after
	}
	missing := fixImports(ctx, string(c.root), changes)
	for i, file := range changes {
		switch {
		case diffs.Len() >= konst.CodemodDiffBytes:
		case bytes.Equal(file.after, spliced[i]):
			diffs.WriteString(siteDiff(file.relative, file.before, found[i].Sites))
		default:
			diffs.WriteString(patch.Unified(file.relative, string(file.before), string(file.after)))
		}
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
	return contract.Result{Content: head + ". applied\n" + listing.String() + clippedDiff(diffs.String()) + missing + c.checked(ctx, args.Lang, changes)}, nil
}

func (c Codemod) relative(path string) string {
	relative, _ := filepath.Rel(string(c.root), path)
	return filepath.ToSlash(relative)
}

func clippedDiff(diff string) string {
	if len(diff) <= konst.CodemodDiffBytes {
		return diff
	}
	return diff[:konst.CodemodDiffBytes] + "\n... diff cut at " + fmt.Sprint(konst.CodemodDiffBytes) + " bytes\n"
}

func applied(sites []catgrep.Site) []catgrep.Site {
	var kept []catgrep.Site
	at := 0
	for _, s := range sites {
		if s.Start >= at {
			kept = append(kept, s)
			at = s.End
		}
	}
	return kept
}

func splice(out *bytes.Buffer, source []byte, from, to int, sites []catgrep.Site) {
	for _, s := range sites {
		out.Write(source[from:s.Start])
		out.WriteString(s.After)
		from = s.End
	}
	out.Write(source[from:to])
}

func siteDiff(relative string, before []byte, sites []catgrep.Site) string {
	around := konst.DiffContextLinesDefault
	lineStarts := []int{0}
	for i, b := range before {
		if b == '\n' && i+1 < len(before) {
			lineStarts = append(lineStarts, i+1)
		}
	}
	lineOf := func(offset int) int {
		line, _ := slices.BinarySearch(lineStarts, offset+1)
		return line - 1
	}
	startOf := func(line int) int {
		if line >= len(lineStarts) {
			return len(before)
		}
		return lineStarts[line]
	}
	changed := slices.DeleteFunc(applied(sites), func(s catgrep.Site) bool { return s.Before == s.After })
	var out strings.Builder
	shift := 0
	for len(changed) > 0 {
		group, last := 1, lineOf(max(changed[0].End-1, changed[0].Start))
		for group < len(changed) && lineOf(changed[group].Start)-last-1 <= 2*around {
			last = lineOf(max(changed[group].End-1, changed[group].Start))
			group++
		}
		head := max(lineOf(changed[0].Start)-around, 0)
		from, to := startOf(head), startOf(last+around+1)
		var after bytes.Buffer
		splice(&after, before, from, to, changed[:group])
		window := patch.Unified(relative, string(before[from:to]), after.String())
		title := len("--- \n+++ \n") + 2*len(relative)
		if out.Len() == 0 {
			out.WriteString(window[:title])
		}
		for line := range strings.Lines(window[title:]) {
			var oldAt, oldCount, newAt, newCount int
			if _, err := fmt.Sscanf(line, "@@ -%d,%d +%d,%d @@", &oldAt, &oldCount, &newAt, &newCount); err != nil {
				out.WriteString(line)
				continue
			}
			fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", oldAt+head, oldCount, newAt+head+shift, newCount)
		}
		shift += bytes.Count(after.Bytes(), []byte("\n")) - bytes.Count(before[from:to], []byte("\n"))
		changed = changed[group:]
	}
	return out.String()
}

func unparsable(language string, found []catgrep.File) error {
	if language != lang.Go.String() {
		return nil
	}
	failures := make([]error, len(found))
	var next atomic.Int64
	var workers sync.WaitGroup
	for range min(catgrep.Workers(), len(found)) {
		workers.Go(func() {
			parser := lang.NewParser(lang.Go)
			var checked bytes.Buffer
			for i := int(next.Add(1) - 1); i < len(found); i = int(next.Add(1) - 1) {
				failures[i] = declarationsFault(parser, &checked, found[i])
			}
		})
	}
	workers.Wait()
	for _, err := range failures {
		if err != nil {
			return err
		}
	}
	return nil
}

func declarationsFault(parser lang.Parser, checked *bytes.Buffer, file catgrep.File) error {
	if file.KeepsParse {
		return nil
	}
	before := file.Source
	checked.Reset()
	checked.WriteString(declarationScope)
	var units []unit
	rows, lines, counted := 1, 1, 0
	for sites := applied(file.Sites); len(sites) > 0; {
		group, from, to := 1, sites[0].UnitStart, sites[0].UnitEnd
		for group < len(sites) && sites[group].UnitStart < to {
			to = max(to, sites[group].UnitEnd)
			group++
		}
		lines += bytes.Count(before[counted:from], []byte("\n"))
		counted = from
		units = append(units, unit{row: rows, line: lines, from: from, to: to})
		written := checked.Len()
		splice(checked, before, from, to, sites[:group])
		checked.WriteByte('\n')
		rows += bytes.Count(checked.Bytes()[written:], []byte("\n"))
		for _, s := range sites[:group] {
			lines += strings.Count(s.After, "\n") - strings.Count(s.Before, "\n")
		}
		sites = sites[group:]
	}
	row, broken := parser.BrokenRow(checked.Bytes())
	if !broken {
		return nil
	}
	text := strings.Split(checked.String(), "\n")[row]
	at := units[0]
	for _, u := range units[1:] {
		if u.row <= row {
			at = u
		}
	}
	checked.Truncate(len(declarationScope))
	checked.Write(before[at.from:at.to])
	if _, wasBroken := parser.BrokenRow(checked.Bytes()); wasBroken {
		return wholeFileFault(parser, file, before)
	}
	return refusal(file.Path, at.line+row-at.row, text)
}

type unit struct{ row, line, from, to int }

const declarationScope = "package scope\n"

func wholeFileFault(parser lang.Parser, file catgrep.File, before []byte) error {
	var after bytes.Buffer
	splice(&after, before, 0, len(before), applied(file.Sites))
	row, broken := parser.BrokenRow(after.Bytes())
	if _, wasBroken := parser.BrokenRow(before); !broken || wasBroken {
		return nil
	}
	return refusal(file.Path, row+1, strings.Split(after.String(), "\n")[row])
}

func refusal(path string, line int, text string) error {
	return fmt.Errorf("the rewrite leaves %s as code that does not parse: line %d reads %s", path, line, strings.TrimSpace(text))
}

func (c Codemod) checked(ctx context.Context, lang string, changes []change) string {
	switch lang {
	case "go":
		packages := []string{}
		for _, file := range changes {
			relative := path.Dir(file.relative)
			pkg := "./" + relative
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
	case "typescript", "tsx", "rust", "python":
		var cold *checkers.Typecheckers
		result, err := cold.Check(ctx, string(c.root))
		if err != nil {
			return "checker: typecheck did not run: " + err.Error() + "\n"
		}
		return "checker: " + clipped(result.Content)
	}
	return "checker: none for lang " + lang + ". run its build or tests to check the change\n"
}

func formatCaptures(l lang.Lang, file catgrep.File) ([]catgrep.Site, error) {
	if l != lang.Rust {
		return nil, nil
	}
	var renames []catgrep.Site
	for _, s := range file.Sites {
		if s.Before != s.After && identifier(s.Before) && identifier(s.After) && !slices.ContainsFunc(renames, func(r catgrep.Site) bool { return r.Before == s.Before }) {
			renames = append(renames, s)
		}
	}
	if len(renames) == 0 {
		return nil, nil
	}
	source, err := os.ReadFile(file.Path)
	if err != nil {
		return nil, err
	}
	root, err := tree.Parse(string(source), lang.Rust)
	if err != nil {
		return nil, err
	}
	defer root.Release()
	formatMacros := []string{"format", "println", "print", "eprintln", "write", "writeln", "panic", "assert", "assert_eq", "assert_ne", "debug_assert", "debug_assert_eq", "debug_assert_ne", "format_args"}
	var captures []catgrep.Site
	for macro := range root.Node().PreOrder() {
		called, named := macro.Field("macro")
		if macro.Kind() != "macro_invocation" || !named || !slices.Contains(formatMacros, called.Text()[strings.LastIndex(called.Text(), ":")+1:]) {
			continue
		}
		function := macro
		for parent, ok := macro.Parent(); ok && function.Kind() != "function_item"; parent, ok = parent.Parent() {
			function = parent
		}
		for _, r := range renames {
			inFunction := slices.ContainsFunc(file.Sites, func(s catgrep.Site) bool {
				return s.Before == r.Before && s.Start >= int(function.Start()) && s.End <= int(function.End())
			})
			if !inFunction {
				continue
			}
			for literal := range macro.PreOrder() {
				if literal.Kind() != "string_literal" && literal.Kind() != "raw_string_literal" {
					continue
				}
				text := source[:literal.End()]
				for at := int(literal.Start()); at < len(text)-1; at++ {
					switch {
					case text[at] != '{':
					case text[at+1] == '{':
						at++
					case bytes.HasPrefix(text[at+1:], []byte(r.Before+"}")) || bytes.HasPrefix(text[at+1:], []byte(r.Before+":")):
						captures = append(captures, catgrep.Site{Start: at + 1, End: at + 1 + len(r.Before), Line: bytes.Count(source[:at], []byte("\n")) + 1, Before: r.Before, After: r.After})
					}
				}
			}
		}
	}
	return captures, nil
}

func identifier(text string) bool {
	return text != "" && !unicode.IsDigit(rune(text[0])) && !strings.ContainsFunc(text, func(r rune) bool { return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) })
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
