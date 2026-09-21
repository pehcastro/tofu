package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/search"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

type Glob struct {
	root turn.Root
}

func NewGlob(dir string) (Glob, error) {
	root, err := turn.NewRoot(dir)
	return Glob{root: root}, err
}

func (g Glob) Name() string { return "glob" }

const ignoredWalkDescription = "it never descends into node_modules, .git or .tofu, " +
	"and it skips every path a .gitignore excludes, reading the one in the working directory and the one in any directory it enters. " +
	"a project instruction file, AGENTS.md or CLAUDE.md, is always listed even when an ignore file excludes it. " +
	"set include_ignored to true to walk the ignored paths too, and the result says the walk was unfiltered"

func (g Glob) Definition() llm.Tool {
	return llm.Tool{
		Name: "glob",
		Description: "lists the files under the turn's working directory whose path matches a shell pattern. " +
			"the pattern is matched against the whole path relative to the working directory and against the file name alone, " +
			"so *.ts finds every typescript file at any depth and src/*.ts finds only the ones directly under src. " +
			ignoredWalkDescription + ". " +
			"it does not read a file and it does not search file contents: search does that",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":         map[string]any{"type": "string"},
				"path":            map[string]any{"type": "string"},
				"include_ignored": map[string]any{"type": "boolean"},
			},
			"required": []string{"pattern"},
		},
	}
}

type globArgs struct {
	Pattern        string `json:"pattern"`
	Path           string `json:"path"`
	IncludeIgnored bool   `json:"include_ignored"`
}

func (g Glob) Run(_ context.Context, raw json.RawMessage) (turn.Result, error) {
	var args globArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("glob: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return turn.Result{}, errors.New("glob: pattern is required")
	}
	if _, err := path.Match(args.Pattern, "probe"); err != nil {
		return turn.Result{}, fmt.Errorf("glob: %q is not a shell pattern: %w", args.Pattern, err)
	}
	under := cmp.Or(args.Path, ".")
	listed, err := filesUnder(g.root, under, args.IncludeIgnored)
	if err != nil {
		return turn.Result{}, fmt.Errorf("glob: %w", err)
	}
	under = listed.under

	var found []string
	for _, rel := range listed.files {
		if matchesPattern(args.Pattern, rel) {
			found = append(found, rel)
		}
	}

	command := "glob " + args.Pattern + " under " + under
	if len(found) == 0 {
		return turn.Result{
			Content: withNote(fmt.Sprintf("no file under %s matches %q, out of %d files searched. the directory exists and was read: this is an answer, not a failure",
				under, args.Pattern, len(listed.files)), listed.note),
			Command: command,
		}, nil
	}
	matched := len(found)
	note := listed.note
	header := fmt.Sprintf("%d of %d files under %s match %q", matched, len(listed.files), under, args.Pattern)
	if matched > konst.GlobPathsResultCap {
		found = found[:konst.GlobPathsResultCap]
		header = fmt.Sprintf("%d of %d files under %s match %q, showing the first %d",
			matched, len(listed.files), under, args.Pattern, konst.GlobPathsResultCap)
		note = strings.TrimSpace(note + " " + search.Note(search.Truncated,
			fmt.Sprintf("%d files matched and %d are shown: narrow the pattern to see the rest", matched, konst.GlobPathsResultCap)))
	}
	return turn.Result{
		Content: withNote(header+"\n"+strings.Join(found, "\n")+"\n", note),
		Command: command,
	}, nil
}

func ListPaths(dir string) ([]string, error) {
	root, err := turn.NewRoot(dir)
	if err != nil {
		return nil, err
	}
	listed, err := filesUnder(root, ".", false)
	return listed.files, err
}

func matchesPattern(pattern, rel string) bool {
	if ok, _ := path.Match(pattern, rel); ok {
		return true
	}
	ok, _ := path.Match(pattern, path.Base(rel))
	return ok
}

func withNote(content, note string) string {
	if note == "" {
		return content
	}
	return strings.TrimSuffix(content, "\n") + "\n" + note + "\n"
}

type listing struct {
	under string
	files []string
	bytes int64
	note  string
}

type ignoreRule struct {
	expression *regexp.Regexp
	literal    string
	anchored   bool
	negated    bool
	dirOnly    bool
}

type ignoreScope struct {
	base  string
	rules []ignoreRule
}

type walk struct {
	root    turn.Root
	scopes  []ignoreScope
	notes   []string
	files   []string
	bytes   int64
	unsized int
}

func filesUnder(root turn.Root, under string, includeIgnored bool) (listing, error) {
	from, err := root.Resolve(under)
	if err != nil {
		return listing{}, err
	}
	walked := walk{root: root}
	if _, err := os.Stat(from); err != nil {
		repaired, note, refusal := repairPath(root, under, true)
		if refusal != nil {
			return listing{}, refusal
		}
		if from, err = root.Resolve(repaired); err != nil {
			return listing{}, err
		}
		under = repaired
		walked.notes = append(walked.notes, note)
	}
	if includeIgnored {
		walked.notes = append(walked.notes, search.Note(search.Unfiltered, "include_ignored was set, so this listing is wider than git's"))
	} else {
		walked.loadAncestors(from)
	}
	walkErr := filepath.WalkDir(from, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "node_modules", ".git", sys.StateDirName, sys.LegacyStateDirName:
				return fs.SkipDir
			}
		}
		rel, err := filepath.Rel(string(root), full)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(rel)
		walked.prune(slash)
		if !entry.IsDir() {
			if projectInstructions(entry.Name()) || !walked.ignored(slash, false) {
				walked.files = append(walked.files, slash)
				info, infoErr := entry.Info()
				if infoErr != nil {
					walked.unsized++
				} else {
					walked.bytes += info.Size()
				}
			}
			return nil
		}
		if full == from {
			return nil
		}
		if walked.ignored(slash, true) {
			return fs.SkipDir
		}
		if !includeIgnored {
			walked.load(full, slash+"/")
		}
		return nil
	})
	if walked.unsized > 0 {
		walked.notes = append(walked.notes, search.Note(search.Partial,
			fmt.Sprintf("%d of the %d files listed could not be measured, so the total size is lower than the real one", walked.unsized, len(walked.files))))
	}
	return listing{under: under, files: walked.files, bytes: walked.bytes, note: strings.Join(walked.notes, " ")}, walkErr
}

func projectInstructions(name string) bool {
	return name == "AGENTS.md" || name == "CLAUDE.md"
}

func (w *walk) loadAncestors(from string) {
	rel, err := filepath.Rel(string(w.root), from)
	if err != nil {
		w.notes = append(w.notes, search.Note(search.Unfiltered, "the path walked is not under the working directory"))
		return
	}
	w.load(string(w.root), "")
	if rel == "." {
		return
	}
	base := ""
	for _, segment := range strings.Split(filepath.ToSlash(rel), "/") {
		base += segment + "/"
		w.load(filepath.Join(string(w.root), filepath.FromSlash(base)), base)
	}
}

func (w *walk) load(dir, base string) {
	name := filepath.Join(dir, ".gitignore")
	file, err := os.Open(name)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	where := cmp.Or(base, "./")
	body, err := io.ReadAll(io.LimitReader(file, konst.IgnoreFileBytesCap+1))
	if err != nil {
		w.notes = append(w.notes, search.Note(search.IgnoreSkipped, "the .gitignore in "+where+" could not be read"))
		return
	}
	if len(body) > konst.IgnoreFileBytesCap {
		w.notes = append(w.notes, search.Note(search.IgnoreSkipped,
			fmt.Sprintf("the .gitignore in %s is over the %d byte cap", where, konst.IgnoreFileBytesCap)))
		return
	}
	scope := ignoreScope{base: base}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimRight(strings.TrimSuffix(line, "\r"), " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule, err := parseIgnoreLine(line)
		if err != nil {
			w.notes = append(w.notes, search.Note(search.IgnoreSkipped,
				fmt.Sprintf("the .gitignore in %s has a pattern tofu does not support, %s: %v", where, line, err)))
			continue
		}
		scope.rules = append(scope.rules, rule)
	}
	if len(scope.rules) > 0 {
		w.scopes = append(w.scopes, scope)
	}
}

func parseIgnoreLine(line string) (ignoreRule, error) {
	if strings.Contains(line, `\`) {
		return ignoreRule{}, errors.New("a backslash escape is not supported")
	}
	pattern, negated := strings.CutPrefix(line, "!")
	pattern, dirOnly := strings.CutSuffix(pattern, "/")
	anchored := strings.Contains(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	if pattern == "" {
		return ignoreRule{}, errors.New("the pattern is empty")
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return ignoreRule{literal: pattern, anchored: anchored, negated: negated, dirOnly: dirOnly}, nil
	}
	expression, err := ignoreExpression(pattern, anchored)
	if err != nil {
		return ignoreRule{}, err
	}
	return ignoreRule{expression: expression, negated: negated, dirOnly: dirOnly}, nil
}

func ignoreExpression(pattern string, anchored bool) (*regexp.Regexp, error) {
	var built strings.Builder
	built.WriteString("^")
	if !anchored {
		built.WriteString("(?:.*/)?")
	}
	segments := strings.Split(pattern, "/")
	for i, segment := range segments {
		last := i == len(segments)-1
		switch {
		case segment == "**" && last:
			built.WriteString(".*")
		case segment == "**":
			built.WriteString("(?:[^/]+/)*")
		default:
			built.WriteString(segmentExpression(segment))
			if !last {
				built.WriteString("/")
			}
		}
	}
	built.WriteString("$")
	return regexp.Compile(built.String())
}

func segmentExpression(segment string) string {
	var built strings.Builder
	for i := 0; i < len(segment); i++ {
		switch segment[i] {
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
			class, negated := strings.CutPrefix(segment[i+1:i+end], "!")
			built.WriteString("[")
			if negated {
				built.WriteString("^")
			}
			built.WriteString(class + "]")
			i += end
		default:
			built.WriteString(regexp.QuoteMeta(segment[i : i+1]))
		}
	}
	return built.String()
}

func (w *walk) prune(rel string) {
	for len(w.scopes) > 0 && !strings.HasPrefix(rel, w.scopes[len(w.scopes)-1].base) {
		w.scopes = w.scopes[:len(w.scopes)-1]
	}
}

func (w *walk) ignored(rel string, isDir bool) bool {
	excluded := false
	for _, scope := range w.scopes {
		within, ok := strings.CutPrefix(rel, scope.base)
		if !ok {
			continue
		}
		for _, rule := range scope.rules {
			if rule.dirOnly && !isDir {
				continue
			}
			matched := rule.expression != nil && rule.expression.MatchString(within)
			if rule.literal != "" {
				matched = within == rule.literal || (!rule.anchored && strings.HasSuffix(within, "/"+rule.literal))
			}
			if matched {
				excluded = !rule.negated
			}
		}
	}
	return excluded
}
