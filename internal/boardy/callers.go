package boardy

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"tofu/internal/konst"
	"tofu/internal/rule"
	"tofu/internal/search"
	"tofu/internal/subagent"
	"tofu/internal/sys"
)

const notOwnedOnPurpose = "not-owned-on-purpose"

var (
	rustPublic = sys.LazyRegexp(`^\s*pub(?:\([^)]*\))?\s+(?:(?:const|async|unsafe|extern\s+"[^"]*")\s+)*(fn|struct|enum|trait|type|union|const|static|mod)\s+([A-Za-z_]\w*)`)
	rustField  = sys.LazyRegexp(`^\s*pub(?:\([^)]*\))?\s+([a-z_]\w*)\s*:[^:]`)
	rustMod    = sys.LazyRegexp(`^\s*(?:pub(?:\([^)]*\))?\s+)?mod\s+\w+\s*;`)
)

type callerItem struct {
	label  string
	kind   string
	word   string
	file   string
	home   string
	near   *regexp.Regexp
	far    *regexp.Regexp
	gate   string
	pkg    string
	decl   *regexp.Regexp
	shadow *regexp.Regexp
}

type callerSite struct {
	item  string
	file  string
	line  int
	text  string
	extra int
	by    string
}

type callerScan struct {
	sites   []callerSite
	stopped string
}

type Impacted struct {
	Item    string   `json:"item"`
	Callers []string `json:"callers,omitempty"`
}

type Impact struct {
	Items   []Impacted `json:"items,omitempty"`
	Unread  []string   `json:"unread,omitempty"`
	Stopped string     `json:"stopped,omitempty"`
}

func ImpactOf(root string, wrote []string) Impact {
	if root == "" {
		return Impact{}
	}
	root, _ = filepath.Abs(root)
	var files []string
	for _, written := range wrote {
		if inside, err := filepath.Rel(root, written); filepath.IsAbs(written) && err == nil && !strings.HasPrefix(inside, "..") {
			written = inside
		}
		if rel := filepath.ToSlash(written); isSource(rel) && !slices.Contains(files, rel) {
			files = append(files, rel)
		}
	}
	if len(files) == 0 {
		return Impact{}
	}
	lines, touched := sourceLines{root: root, read: map[string][]string{}}, changedIn(root, files)
	declared := search.Index(root, files, func(string) bool { return true })
	impact := Impact{Unread: declared.Unread}
	var changed []search.Definition
	for _, definition := range declared.Definitions {
		head := lines.head(definition)
		exported := map[string]bool{".go": ast.IsExported(bareName(definition.Symbol)), ".rs": strings.HasPrefix(head, "pub"), ".ts": strings.HasPrefix(head, "export")}
		if exported[languageOf(definition.Path)] && touched(definition) {
			changed = append(changed, definition)
		}
	}
	if len(changed) == 0 {
		return impact
	}
	sources, stopped := callerSources(root)
	if sources == nil {
		sources, stopped = walkedSources(root)
	}
	graph := search.Index(root, sources, func(name string) bool {
		return slices.ContainsFunc(changed, func(definition search.Definition) bool { return bareName(definition.Symbol) == name })
	})
	impact.Stopped = stopped
	for _, unread := range graph.Unread {
		if !slices.Contains(impact.Unread, unread) {
			impact.Unread = append(impact.Unread, unread)
		}
	}
	module := goModule(root)
	for _, definition := range changed {
		item := Impacted{Item: string(definition.Kind) + " " + definition.Symbol + " in " + definition.Path}
		if callee, resolvable := goCallee(root, module, definition); resolvable {
			item.Callers = callee.impacted(root, sources)
		} else {
			item.Callers = impactedBy(graph, definition)
		}
		impact.Items = append(impact.Items, item)
	}
	return impact
}

func goCallee(root, module string, definition search.Definition) (callerItem, bool) {
	if definition.Kind != search.KindFunc && definition.Kind != search.KindMethod || languageOf(definition.Path) != ".go" {
		return callerItem{}, false
	}
	raw, err := os.ReadFile(filepath.Join(root, definition.Path))
	if err != nil {
		return callerItem{}, false
	}
	items := goItems(definition.Path, raw, module)
	found := slices.IndexFunc(items, func(item callerItem) bool {
		return item.kind == string(definition.Kind) && item.word == bareName(definition.Symbol)
	})
	if found < 0 {
		return callerItem{}, false
	}
	return items[found], true
}

func (item callerItem) impacted(root string, sources []string) []string {
	var callers []string
	for _, rel := range sources {
		if rel == item.file {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		if site, found := item.use(rel, raw); found {
			callers = append(callers, fmt.Sprintf("%s:%d%s", site.file, site.line, site.more()))
		}
	}
	return callers
}

func impactedBy(graph search.Graph, definition search.Definition) []string {
	name := bareName(definition.Symbol)
	var callers []string
	at, more := map[string]int{}, []int{}
	for _, site := range graph.Callers {
		if site.Name != name || site.Path == definition.Path || languageOf(site.Path) != languageOf(definition.Path) ||
			slices.ContainsFunc(graph.Definitions, func(other search.Definition) bool { return other.Path == site.Path && bareName(other.Symbol) == name }) {
			continue
		}
		if seen, found := at[site.Path]; found {
			more[seen]++
			continue
		}
		at[site.Path] = len(callers)
		callers, more = append(callers, fmt.Sprintf("%s:%d", site.Path, site.Line)), append(more, 0)
	}
	for i, extra := range more {
		if extra > 0 {
			callers[i] += fmt.Sprintf(" (+%d more)", extra)
		}
	}
	return callers
}

func languageOf(rel string) string {
	return strings.TrimSuffix(path.Ext(rel), "x")
}

func isSource(rel string) bool {
	return slices.Contains([]string{".go", ".rs", ".ts", ".tsx"}, path.Ext(rel))
}

func scanStopped(count int, at string) string {
	return fmt.Sprintf("the scan stopped after %d files, at %s, so the list may be short", count, at)
}

func walkedSources(root string) ([]string, string) {
	var sources []string
	stopped := ""
	err := filepath.WalkDir(root, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		switch {
		case entry.IsDir() && full != root && (name == "vendor" || name == "node_modules" || strings.HasPrefix(name, ".")):
			return fs.SkipDir
		case entry.IsDir() || !isSource(name):
			return nil
		}
		rel, err := filepath.Rel(root, full)
		if len(sources) == konst.BoardCallerFilesMost {
			stopped = scanStopped(len(sources), filepath.ToSlash(rel))
			return fs.SkipAll
		}
		sources = append(sources, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		return sources, "the walk for callers stopped, so the list may be short: " + err.Error()
	}
	return sources, stopped
}

func changedIn(root string, files []string) func(search.Definition) bool {
	tracked, listErr := gitOut(root, "", append([]string{"ls-tree", "-r", "--name-only", "HEAD", "--"}, files...)...)
	diff, diffErr := gitOut(root, "", append([]string{"diff", "-U0", "--no-color", "--no-ext-diff", "--relative", "HEAD", "--"}, files...)...)
	if listErr != nil || diffErr != nil {
		return func(search.Definition) bool { return true }
	}
	hunks := map[string][][2]int{}
	current := ""
	for line := range strings.Lines(diff) {
		fields := strings.Fields(line)
		switch {
		case strings.HasPrefix(line, "+++ "):
			current = strings.TrimPrefix(strings.TrimSpace(line[4:]), "b/")
		case strings.HasPrefix(line, "@@ ") && len(fields) > 2:
			start, count, counted := strings.Cut(strings.TrimPrefix(fields[2], "+"), ",")
			first, _ := strconv.Atoi(start)
			last := first
			if spanned, _ := strconv.Atoi(count); counted && spanned > 1 {
				last = first + spanned - 1
			}
			hunks[current] = append(hunks[current], [2]int{first, last})
		}
	}
	known := strings.Split(filepath.ToSlash(tracked), "\n")
	return func(definition search.Definition) bool {
		return !slices.Contains(known, definition.Path) || slices.ContainsFunc(hunks[definition.Path], func(hunk [2]int) bool {
			return hunk[0] <= definition.LastLine && definition.FirstLine <= hunk[1]
		})
	}
}

type sourceLines struct {
	root string
	read map[string][]string
}

func (s sourceLines) line(rel string, number int) string {
	if s.read[rel] == nil {
		raw, _ := os.ReadFile(filepath.Join(s.root, rel))
		s.read[rel] = strings.Split(string(raw), "\n")
	}
	if number > len(s.read[rel]) {
		return ""
	}
	return strings.TrimSpace(s.read[rel][number-1])
}

func (s sourceLines) head(definition search.Definition) string {
	head := definition.FirstLine
	for head < definition.LastLine && strings.HasPrefix(s.line(definition.Path, head), "#[") {
		head++
	}
	return s.line(definition.Path, head)
}

func (m Managed) callersOutside(ticket Ticket) (callerScan, error) {
	if m.Store.Root == "" || len(ticket.Owns) == 0 {
		return callerScan{}, nil
	}
	return scanCallers(m.Store.Root, ticket)
}

func (m Managed) waive(hooks []rule.Hook, s subject, reason string) (string, string, error) {
	held, s := m.holding(hooks, rule.WhenBefore, s)
	at := slices.IndexFunc(held, func(h rule.Hook) bool {
		return runsCheck(h, checkCallersOwned) && h.Bypass != nil && slices.Contains(h.Bypass.Who, m.actorKind(s.actor))
	})
	if at < 0 {
		return reason, "", nil
	}
	scan, err := m.callersOutside(s.ticket)
	if err != nil || len(scan.sites) == 0 && scan.stopped == "" {
		return reason, "", err
	}
	waived := ""
	if len(scan.sites) > 0 {
		waived = held[at].ID
	}
	return reason + " (" + scan.recorded() + ")", waived, nil
}

func callerSources(root string) ([]string, string) {
	listed, err := gitOut(root, "", "ls-files", "--cached", "--others", "--exclude-standard", "--", "*.go", "*.rs", "*.ts", "*.tsx")
	if err != nil {
		return nil, "the scan for callers did not run: " + err.Error()
	}
	var sources []string
	for rel := range strings.Lines(listed) {
		rel = strings.TrimSpace(rel)
		if slices.ContainsFunc(strings.Split(rel, "/"), func(segment string) bool { return segment == "vendor" || strings.HasPrefix(segment, ".") }) {
			continue
		}
		if len(sources) == konst.BoardCallerFilesMost {
			return sources, scanStopped(len(sources), rel)
		}
		sources = append(sources, rel)
	}
	return sources, ""
}

func scanCallers(root string, ticket Ticket) (callerScan, error) {
	sources, stopped := callerSources(root)
	scan := callerScan{stopped: stopped}
	owns := rootedOwns(root, ticket.Owns)
	owned := func(rel string) bool {
		matched, err := subagent.Matches(rel, owns)
		return matched && err == nil
	}
	read := func(rel string) ([]byte, error) {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return raw, err
	}
	text := ticket.Title + "\n" + ticket.Scope + "\n" + strings.Join(subagent.InForce(ticket.Acceptance), "\n")
	module := goModule(root)
	var items []callerItem
	for _, rel := range sources {
		if !owned(rel) || path.Ext(rel) != ".go" && path.Ext(rel) != ".rs" {
			continue
		}
		raw, err := read(rel)
		if err != nil {
			return scan, err
		}
		declared := rustItems(rel, raw)
		if path.Ext(rel) == ".go" {
			declared = goItems(rel, raw, module)
		}
		for _, item := range declared {
			if named(text, item.label) {
				items = append(items, item)
			}
		}
	}
	for _, glob := range owns {
		if site, found := unregistered(root, glob, owned); found {
			scan.sites = append(scan.sites, site)
		}
	}
	for _, rel := range sources {
		if owned(rel) || !slices.ContainsFunc(items, func(item callerItem) bool { return path.Ext(item.file) == path.Ext(rel) }) {
			continue
		}
		raw, err := read(rel)
		if err != nil {
			return scan, err
		}
		for _, item := range items {
			if site, found := item.use(rel, raw); found {
				scan.sites = append(scan.sites, site)
			}
		}
	}
	scan.sites = append(scan.sites, functionCallers(root, sources, owned, text)...)
	slices.SortStableFunc(scan.sites, func(a, b callerSite) int { return strings.Compare(a.item, b.item) })
	return scan, nil
}

func functionCallers(root string, sources []string, owned func(string) bool, text string) []callerSite {
	var mine, others []string
	for _, rel := range sources {
		if path.Ext(rel) == ".go" {
			continue
		}
		if owned(rel) {
			mine = append(mine, rel)
		} else {
			others = append(others, rel)
		}
	}
	lines := sourceLines{root: root, read: map[string][]string{}}
	home := map[string]string{}
	for _, definition := range search.Index(root, mine, func(string) bool { return true }).Definitions {
		name := bareName(definition.Symbol)
		public := definition.Kind == search.KindFunc && path.Ext(definition.Path) == ".rs" && strings.HasPrefix(lines.head(definition), "pub")
		if home[name] == "" && (public || named(text, "fn "+name) && (definition.Kind == search.KindFunc || definition.Kind == search.KindMethod)) {
			home[name] = definition.Path
		}
	}
	if len(home) == 0 {
		return nil
	}
	graph := search.Index(root, others, func(name string) bool { return home[name] != "" })
	definedIn := map[string][]string{}
	for _, definition := range graph.Definitions {
		name := bareName(definition.Symbol)
		definedIn[name] = append(definedIn[name], definition.Path)
	}
	crates := map[string]string{}
	crate := func(rel string) string {
		if _, known := crates[path.Dir(rel)]; !known {
			crates[path.Dir(rel)] = crateOf(root, rel)
		}
		return crates[path.Dir(rel)]
	}
	var sites []callerSite
	at := map[string]int{}
	for _, call := range graph.Callers {
		from := home[call.Name]
		rust := path.Ext(from) == ".rs"
		inCrate := rust && crate(from) != "" && crate(call.Path) == crate(from) && !slices.ContainsFunc(definedIn[call.Name], func(rel string) bool { return crate(rel) == crate(from) })
		if rust != (path.Ext(call.Path) == ".rs") || slices.Contains(definedIn[call.Name], call.Path) || !inCrate && !named(text, "fn "+call.Name) {
			continue
		}
		item := "fn " + call.Name + " (" + from + ")"
		if seen, found := at[item+"\n"+call.Path]; found {
			sites[seen].extra++
			continue
		}
		at[item+"\n"+call.Path] = len(sites)
		sites = append(sites, callerSite{item: item, file: call.Path, line: call.Line, text: clipped(lines.line(call.Path, call.Line))})
	}
	return sites
}

func bareName(symbol string) string {
	return symbol[strings.LastIndex(symbol, ".")+1:]
}

func crateOf(root, rel string) string {
	for dir := path.Dir(rel); ; dir = path.Dir(dir) {
		if _, err := os.Stat(filepath.Join(root, dir, "Cargo.toml")); err == nil {
			return dir
		}
		if dir == "." {
			return ""
		}
	}
}

func rootedOwns(root string, owns []string) []string {
	var out []string
	for _, glob := range owns {
		if rel, err := filepath.Rel(root, glob); filepath.IsAbs(glob) && err == nil && !strings.HasPrefix(rel, "..") {
			glob = rel
		}
		out = append(out, filepath.ToSlash(glob))
	}
	return out
}

func goModule(root string) string {
	raw, _ := os.ReadFile(filepath.Join(root, "go.mod"))
	for line := range strings.Lines(string(raw)) {
		if module, found := strings.CutPrefix(strings.TrimSpace(line), "module "); found {
			return strings.TrimSpace(module)
		}
	}
	return ""
}

func named(text, label string) bool {
	_, name, _ := strings.Cut(label, " ")
	if owner, field, isField := strings.Cut(name, "."); isField {
		return regexp.MustCompile(`\b` + owner + `[.:]+` + field + `\b`).MatchString(text)
	}
	if unicode.IsUpper(rune(name[0])) {
		return regexp.MustCompile(`\b` + name + `\b`).MatchString(text)
	}
	return regexp.MustCompile(`\b` + name + `\(|[.:]` + name + "\\b|`" + name + "`").MatchString(text)
}

func goItems(rel string, raw []byte, module string) []callerItem {
	file, err := parser.ParseFile(token.NewFileSet(), rel, raw, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	home, gate := "", ""
	if module != "" {
		home = strings.TrimSuffix(module+"/"+path.Dir(rel), "/.")
		gate = strconv.Quote(home)
	}
	var items []callerItem
	add := func(kind, label, word, near, far, gate string) {
		decl := regexp.MustCompile(`^\s*func\s+(\([^)]*\)\s*)?` + word + `\b`)
		items = append(items, callerItem{label: kind + " " + label, kind: kind, word: word, file: rel, home: home, near: regexp.MustCompile(near), far: regexp.MustCompile(far), gate: gate, pkg: file.Name.Name, decl: decl})
	}
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			name := decl.Name.Name
			switch {
			case !ast.IsExported(name):
			case decl.Recv != nil:
				add("method", name, name, `\.`+name+`\(`, `\.`+name+`\(`, "")
			default:
				add("func", name, name, `\b`+name+`\(`, `\.`+name+`\(`, gate)
			}
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					if !spec.Name.IsExported() {
						continue
					}
					add("type", spec.Name.Name, spec.Name.Name, `\b`+spec.Name.Name+`\b`, `\.`+spec.Name.Name+`\b`, gate)
					var fields []*ast.Field
					if shape, isStruct := spec.Type.(*ast.StructType); isStruct {
						fields = shape.Fields.List
					}
					for _, field := range fields {
						for _, name := range field.Names {
							if name.IsExported() {
								use := `\.` + name.Name + `\b|\b` + name.Name + `:\s`
								add("field", spec.Name.Name+"."+name.Name, name.Name, use, use, "")
							}
						}
					}
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						if name.IsExported() {
							add("value", name.Name, name.Name, `\b`+name.Name+`\b`, `\.`+name.Name+`\b`, gate)
						}
					}
				}
			}
		}
	}
	return items
}

func rustItems(rel string, raw []byte) []callerItem {
	var items []callerItem
	owner := ""
	for line := range strings.Lines(string(raw)) {
		if strings.TrimSpace(line) == "}" {
			owner = ""
		}
		if found := rustPublic().FindStringSubmatch(line); found != nil {
			kind, name := found[1], found[2]
			if kind == "fn" {
				continue
			}
			use := `\b` + name + `\b`
			shadow := regexp.MustCompile(`\b(?:enum|struct|trait|type|union|mod|const|static)\s+` + name + `\b`)
			switch kind {
			case "enum":
				use = `\b` + name + `::\w`
			case "mod":
				use = `\b` + name + `::`
			}
			if kind == "struct" && strings.Contains(line, "{") {
				owner = name
			}
			pattern := regexp.MustCompile(use)
			items = append(items, callerItem{label: kind + " " + name, word: name, file: rel, near: pattern, far: pattern, shadow: shadow})
			continue
		}
		if found := rustField().FindStringSubmatch(line); found != nil && owner != "" {
			pattern := regexp.MustCompile(`\.` + found[1] + `\b`)
			items = append(items, callerItem{label: "field " + owner + "." + found[1], word: found[1], file: rel, near: pattern, far: pattern})
		}
	}
	return items
}

func (item callerItem) use(rel string, raw []byte) (callerSite, bool) {
	pattern := item.far
	switch {
	case path.Ext(rel) != path.Ext(item.file), !bytes.Contains(raw, []byte(item.word)), item.shadow != nil && item.shadow.Match(raw):
		return callerSite{}, false
	case path.Dir(rel) == path.Dir(item.file):
		pattern = item.near
	case item.gate != "":
		imported := regexp.MustCompile(`(?m)^\s*(?:import\s+)?([\w.]+\s+)?` + regexp.QuoteMeta(item.gate)).FindSubmatch(raw)
		if imported == nil || string(bytes.TrimSpace(imported[1])) == "_" {
			return callerSite{}, false
		}
		pattern = item.near
		if qualifier := cmp.Or(string(bytes.TrimSpace(imported[1])), item.pkg); qualifier != "." {
			pattern = regexp.MustCompile(`\b` + qualifier + item.far.String())
		}
	}
	site := callerSite{item: item.label + " (" + item.file + ")", file: rel}
	if item.kind == "func" || item.kind == "method" {
		files := token.NewFileSet()
		file, err := parser.ParseFile(files, rel, raw, parser.SkipObjectResolution)
		if err == nil {
			return item.goCalls(site, files, file, raw)
		}
		site.by = "the file does not parse"
	}
	for number, line := range strings.Split(string(raw), "\n") {
		if !strings.Contains(line, item.word) || strings.HasPrefix(strings.TrimSpace(line), "//") || item.decl != nil && item.decl.MatchString(line) || !pattern.MatchString(line) {
			continue
		}
		if site.line == 0 {
			site.line, site.text = number+1, clipped(strings.TrimSpace(line))
		} else {
			site.extra++
		}
	}
	return site, site.line > 0
}

func (item callerItem) goCalls(site callerSite, files *token.FileSet, file *ast.File, raw []byte) (callerSite, bool) {
	imports := map[string]string{}
	for _, spec := range file.Imports {
		imported, _ := strconv.Unquote(spec.Path.Value)
		name := path.Base(imported)
		if imported == item.home {
			name = item.pkg
		}
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imports[name] = imported
	}
	inside := path.Dir(site.file) == path.Dir(item.file) && file.Name.Name == item.pkg || item.home != "" && imports["."] == item.home
	lines := strings.Split(string(raw), "\n")
	ast.Inspect(file, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		by, calls := item.calledBy(call.Fun, imports, inside)
		if !calls {
			return true
		}
		if line := files.Position(call.Lparen).Line; site.line == 0 {
			site.line, site.text = line, clipped(strings.TrimSpace(lines[line-1]))
		} else {
			site.extra++
		}
		site.by = cmp.Or(site.by, by)
		return true
	})
	return site, site.line > 0
}

func (item callerItem) calledBy(called ast.Expr, imports map[string]string, inside bool) (string, bool) {
	switch called := called.(type) {
	case *ast.Ident:
		return "", item.kind == "func" && inside && called.Name == item.word
	case *ast.SelectorExpr:
		if called.Sel.Name != item.word {
			return "", false
		}
		qualifier, isIdent := called.X.(*ast.Ident)
		switch {
		case isIdent && imports[qualifier.Name] != "" && item.home == "":
			return "the module path is not known", item.kind == "func" && qualifier.Name == item.pkg
		case isIdent && imports[qualifier.Name] != "":
			return "", item.kind == "func" && imports[qualifier.Name] == item.home
		case item.kind == "method":
			return "the receiver type is not resolved", true
		}
	}
	return "", false
}

func unregistered(root, glob string, owned func(string) bool) (callerSite, bool) {
	if path.Ext(glob) != ".rs" || strings.ContainsAny(glob, "*?[") {
		return callerSite{}, false
	}
	if _, err := os.Stat(filepath.Join(root, glob)); err == nil {
		return callerSite{}, false
	}
	dir, module := path.Dir(glob), strings.TrimSuffix(path.Base(glob), ".rs")
	if module == "mod" {
		dir, module = path.Dir(dir), path.Base(dir)
	}
	for _, registrar := range []string{dir + "/mod.rs", dir + "/lib.rs", dir + "/main.rs", dir + ".rs"} {
		raw, err := os.ReadFile(filepath.Join(root, registrar))
		if err != nil {
			continue
		}
		if owned(registrar) || regexp.MustCompile(`\bmod\s+`+module+`\s*;`).Match(raw) {
			return callerSite{}, false
		}
		site := callerSite{item: "mod " + module + " (new, " + glob + ")", file: registrar, line: 1, text: "registers the new module"}
		for number, line := range strings.Split(string(raw), "\n") {
			if rustMod().MatchString(line) {
				site.line, site.text = number+1, clipped(strings.TrimSpace(line))
			}
		}
		return site, true
	}
	return callerSite{}, false
}

func clipped(line string) string {
	runes := []rune(line)
	if len(runes) <= konst.BoardCallerLineRunesMost {
		return line
	}
	return string(runes[:konst.BoardCallerLineRunesMost]) + "..."
}

func (scan callerScan) refusal(id string) string {
	files := map[string]bool{}
	var lines []string
	last := ""
	for _, site := range scan.sites {
		files[site.file] = true
		if site.item != last {
			lines, last = append(lines, "  "+site.item), site.item
		}
		lines = append(lines, fmt.Sprintf("    %s:%d: %s%s", site.file, site.line, site.text, site.more()))
	}
	tail := fmt.Sprintf("own them with tofu boardy set %s --owns <its owns and these files>, or move it with --reason %s to record them on the ticket", id, notOwnedOnPurpose)
	if scan.stopped != "" {
		tail = scan.stopped + "\n" + tail
	}
	noun := "files"
	if len(files) == 1 {
		noun = "file"
	}
	return fmt.Sprintf("what it names is used in %d %s outside its owns, which a move to %s would leave unowned:\n%s\n%s", len(files), noun, Doing, strings.Join(lines, "\n"), tail)
}

func (site callerSite) more() string {
	more := ""
	if site.extra > 0 {
		more = fmt.Sprintf(" (+%d more)", site.extra)
	}
	if site.by != "" {
		more += ", matched by name: " + site.by
	}
	return more
}

func (scan callerScan) recorded() string {
	var parts []string
	last := ""
	for _, site := range scan.sites {
		if name, _, _ := strings.Cut(site.item, " ("); name != last {
			parts, last = append(parts, name+":"), name
		}
		parts = append(parts, fmt.Sprintf("%s:%d", site.file, site.line))
	}
	if scan.stopped != "" {
		parts = append(parts, scan.stopped)
	}
	return "outside owns " + strings.Join(parts, " ")
}
