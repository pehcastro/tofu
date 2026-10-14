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
	"runtime"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

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
			"** matches any number of directories, so src/**/*.ts finds every typescript file under src, and {ts,tsx} matches either. " +
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
	glob, err := globExpression(args.Pattern)
	if err != nil {
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
		if glob.MatchString(rel) || glob.MatchString(path.Base(rel)) {
			found = append(found, rel)
		}
	}

	command := args.Pattern + " under " + under
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

func globExpression(pattern string) (*regexp.Regexp, error) {
	if !strings.Contains(pattern, "/") {
		pattern = strings.ReplaceAll(pattern, `\`, "/")
	}
	for strings.HasPrefix(pattern, "./") {
		pattern = pattern[2:]
	}
	var built strings.Builder
	built.WriteString("(?s)^")
	var braces []int
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; {
		case c == '\\':
			if i+1 == len(pattern) {
				return nil, fmt.Errorf("the backslash at byte %d escapes nothing", i)
			}
			i++
			built.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		case c == '*' && globstar(pattern, i, len(braces) > 0):
			i++
			if i+1 < len(pattern) && pattern[i+1] == '/' {
				built.WriteString("(?:[^/]+/)*")
				i++
			} else {
				built.WriteString(".*")
			}
		case c == '*':
			built.WriteString("[^/]*")
		case c == '?':
			built.WriteString("[^/]")
		case c == '[':
			end, err := globClass(&built, pattern, i)
			if err != nil {
				return nil, err
			}
			i = end
		case c == '{':
			braces = append(braces, i)
			built.WriteString("(?:")
		case c == '}' && len(braces) > 0:
			braces = braces[:len(braces)-1]
			built.WriteString(")")
		case c == ',' && len(braces) > 0:
			built.WriteString("|")
		default:
			built.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	if len(braces) > 0 {
		return nil, fmt.Errorf("the brace at byte %d is never closed", braces[0])
	}
	return regexp.Compile(built.String() + "$")
}

func globstar(pattern string, at int, inGroup bool) bool {
	if at+1 >= len(pattern) || pattern[at+1] != '*' {
		return false
	}
	opens := at == 0 || pattern[at-1] == '/' || inGroup && strings.IndexByte("{,", pattern[at-1]) >= 0
	closes := at+2 == len(pattern) || pattern[at+2] == '/' || inGroup && strings.IndexByte("},", pattern[at+2]) >= 0
	return opens && closes
}

func globClass(built *strings.Builder, pattern string, start int) (int, error) {
	i := start + 1
	built.WriteString("[")
	if i < len(pattern) && pattern[i] == '^' {
		built.WriteString("^")
		i++
	}
	for first := i; ; {
		if i < len(pattern) && pattern[i] == ']' {
			if i == first {
				return 0, fmt.Errorf("the class at byte %d is empty", start)
			}
			built.WriteString("]")
			return i, nil
		}
		low, next, err := classRune(pattern, i, start)
		if err != nil {
			return 0, err
		}
		built.WriteString(low)
		i = next
		if i < len(pattern) && pattern[i] == '-' {
			high, next, err := classRune(pattern, i+1, start)
			if err != nil {
				return 0, err
			}
			built.WriteString("-" + high)
			i = next
		}
	}
}

func classRune(pattern string, i, class int) (string, int, error) {
	if i < len(pattern) && pattern[i] == '\\' {
		i++
	} else if i < len(pattern) && (pattern[i] == '-' || pattern[i] == ']') {
		return "", 0, fmt.Errorf("the class at byte %d has a bare %c where a character belongs", class, pattern[i])
	}
	if i >= len(pattern) {
		return "", 0, fmt.Errorf("the class at byte %d is never closed", class)
	}
	r, size := utf8.DecodeRuneInString(pattern[i:])
	if r < utf8.RuneSelf && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
		return `\` + string(r), i + size, nil
	}
	return string(r), i + size, nil
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
	sizes []int64
	bytes int64
	note  string
}

type ignoreRule struct {
	expression *regexp.Regexp
	nameOnly   bool
	head       string
	tail       string
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
	root        turn.Root
	from        string
	unfiltered  bool
	ahead       map[string]chan dirListing
	readers     chan struct{}
	scopes      []ignoreScope
	notes       []string
	files       []string
	sizes       []int64
	bytes       int64
	unsized     int
	notFollowed int
}

func filesUnder(root turn.Root, under string, includeIgnored bool) (listing, error) {
	from, err := root.Resolve(under)
	if err != nil {
		return listing{}, err
	}
	walked := walk{root: root, unfiltered: includeIgnored,
		ahead: map[string]chan dirListing{}, readers: make(chan struct{}, runtime.GOMAXPROCS(0))}
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
	const enter = string(filepath.Separator)
	if info, err := os.Stat(from); err == nil && info.IsDir() {
		from = strings.TrimSuffix(from, enter) + enter
	}
	walked.from = from
	start, walkErr := os.Lstat(from)
	if walkErr == nil {
		walkErr = walked.visit(from, fs.FileInfoToDirEntry(start))
	}
	if walked.notFollowed > 0 {
		walked.notes = append(walked.notes, fmt.Sprintf("links not followed: %d, because each leads outside the working directory or nowhere.", walked.notFollowed))
	}
	if walked.unsized > 0 {
		walked.notes = append(walked.notes, search.Note(search.Partial,
			fmt.Sprintf("%d of the %d files listed could not be measured, so the total size is lower than the real one", walked.unsized, len(walked.files))))
	}
	return listing{under: under, files: walked.files, sizes: walked.sizes, bytes: walked.bytes, note: strings.Join(walked.notes, " ")}, walkErr
}

func (w *walk) visit(full string, entry fs.DirEntry) error {
	if entry.IsDir() {
		if neverWalked(entry.Name()) {
			return nil
		}
		if full == w.from {
			return w.descend(full, "")
		}
	}
	rel, err := filepath.Rel(string(w.root), full)
	if err != nil {
		return err
	}
	slash := filepath.ToSlash(rel)
	w.prune(slash)
	if entry.IsDir() {
		if w.ignored(slash, true) {
			return nil
		}
		return w.descend(full, slash+"/")
	}
	if !projectInstructions(entry.Name()) && w.ignored(slash, false) {
		return nil
	}
	info, infoErr := entry.Info()
	if entry.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		info, infoErr = os.Stat(full)
		if infoErr == nil && info.IsDir() {
			return nil
		}
		if _, refused := w.root.Resolve(rel); infoErr != nil || refused != nil {
			w.notFollowed++
			return nil
		}
	}
	w.files = append(w.files, slash)
	if infoErr != nil {
		w.unsized++
		w.sizes = append(w.sizes, -1)
	} else {
		w.bytes += info.Size()
		w.sizes = append(w.sizes, info.Size())
	}
	return nil
}

func (w *walk) descend(dir, base string) error {
	entries, err := w.list(dir)
	if err != nil {
		return err
	}
	mayHoldIgnoreFile := slices.ContainsFunc(entries, func(entry fs.DirEntry) bool {
		return strings.EqualFold(entry.Name(), ".gitignore")
	})
	if mayHoldIgnoreFile && base != "" && !w.unfiltered {
		w.load(dir, base)
	}
	for _, entry := range entries {
		if entry.IsDir() && !neverWalked(entry.Name()) {
			w.readAhead(filepath.Join(dir, entry.Name()))
		}
	}
	for _, entry := range entries {
		if err := w.visit(filepath.Join(dir, entry.Name()), entry); err != nil {
			return err
		}
	}
	return nil
}

type dirListing struct {
	entries []fs.DirEntry
	err     error
}

func (w *walk) readAhead(dir string) {
	listed := make(chan dirListing, 1)
	w.ahead[dir] = listed
	go func() {
		w.readers <- struct{}{}
		entries, err := os.ReadDir(dir)
		<-w.readers
		listed <- dirListing{entries: entries, err: err}
	}()
}

func (w *walk) list(dir string) ([]fs.DirEntry, error) {
	listed, ok := w.ahead[dir]
	if !ok {
		return os.ReadDir(dir)
	}
	delete(w.ahead, dir)
	read := <-listed
	return read.entries, read.err
}

func neverWalked(dir string) bool {
	switch dir {
	case "node_modules", ".git", sys.StateDirName, sys.LegacyStateDirName:
		return true
	}
	return false
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
	expression, nameOnly, err := ignoreExpression(pattern, anchored)
	if err != nil {
		return ignoreRule{}, err
	}
	rule := ignoreRule{expression: expression, nameOnly: nameOnly, negated: negated, dirOnly: dirOnly,
		tail: pattern[strings.LastIndexAny(pattern, "*?]")+1:]}
	if anchored || nameOnly {
		rule.head = pattern[:strings.IndexAny(pattern, "*?[")]
	}
	return rule, nil
}

func (r ignoreRule) matches(path string) bool {
	return strings.HasPrefix(path, r.head) && strings.HasSuffix(path, r.tail) && r.expression.MatchString(path)
}

func ignoreExpression(pattern string, anchored bool) (*regexp.Regexp, bool, error) {
	var built strings.Builder
	crossesSlash := false
	segments := strings.Split(pattern, "/")
	for i, segment := range segments {
		last := i == len(segments)-1
		switch {
		case segment == "**" && last:
			built.WriteString(".*")
		case segment == "**":
			built.WriteString("(?:[^/]+/)*")
		default:
			expression, crosses := segmentExpression(segment)
			crossesSlash = crossesSlash || crosses
			built.WriteString(expression)
			if !last {
				built.WriteString("/")
			}
		}
	}
	nameOnly, start := !anchored && !crossesSlash, "^"
	if !anchored && crossesSlash {
		start = "^(?:.*/)?"
	}
	expression, err := regexp.Compile(start + built.String() + "$")
	return expression, nameOnly, err
}

func segmentExpression(segment string) (string, bool) {
	crossesSlash := false
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
			members, negated := strings.CutPrefix(segment[i+1:i+end], "!")
			if negated {
				members = "^" + members
			}
			class, err := regexp.Compile("[" + members + "]")
			crossesSlash = crossesSlash || err != nil || class.MatchString("/")
			built.WriteString("[" + members + "]")
			i += end
		default:
			built.WriteString(regexp.QuoteMeta(segment[i : i+1]))
		}
	}
	return built.String(), crossesSlash
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
			var matched bool
			switch {
			case rule.literal != "":
				matched = within == rule.literal || (!rule.anchored && strings.HasSuffix(within, "/"+rule.literal))
			case rule.nameOnly:
				parent := strings.LastIndexByte(within, '/')
				matched = !strings.Contains(within[:max(parent, 0)], "\n") && rule.matches(within[parent+1:])
			default:
				matched = rule.matches(within)
			}
			if matched {
				excluded = !rule.negated
			}
		}
	}
	return excluded
}
