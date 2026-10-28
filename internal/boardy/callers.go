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
	"strings"
	"unicode"

	"tofu/internal/konst"
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
	word   string
	file   string
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
}

type callerScan struct {
	sites   []callerSite
	stopped string
}

func (m Managed) ownedCallers(ticket Ticket, reason string) (string, error) {
	if m.Store.Root == "" || len(ticket.Owns) == 0 {
		return reason, nil
	}
	scan, err := scanCallers(m.Store.Root, ticket)
	if err != nil || len(scan.sites) == 0 && scan.stopped == "" {
		return reason, err
	}
	switch {
	case strings.HasPrefix(reason, notOwnedOnPurpose):
		return reason + " (" + scan.recorded() + ")", nil
	case len(scan.sites) > 0:
		return reason, RefusedError{Ticket: ticket.ID, Why: scan.refusal(ticket.ID)}
	}
	return strings.TrimSpace(reason + " (" + scan.stopped + ")"), nil
}

func callerSources(root string) ([]string, string) {
	listed, err := gitOut(root, "", "ls-files", "--cached", "--others", "--exclude-standard", "--", "*.go", "*.rs")
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
			return sources, fmt.Sprintf("the scan stopped after %d files, at %s, so the list may be short", len(sources), rel)
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
		if !owned(rel) {
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
	slices.SortStableFunc(scan.sites, func(a, b callerSite) int { return strings.Compare(a.item, b.item) })
	return scan, nil
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
	gate := ""
	if module != "" {
		gate = `"` + strings.TrimSuffix(module+"/"+path.Dir(rel), "/.") + `"`
	}
	var items []callerItem
	add := func(kind, label, word, near, far, gate string) {
		decl := regexp.MustCompile(`^\s*func\s+(\([^)]*\)\s*)?` + word + `\b`)
		items = append(items, callerItem{label: kind + " " + label, word: word, file: rel, near: regexp.MustCompile(near), far: regexp.MustCompile(far), gate: gate, pkg: file.Name.Name, decl: decl})
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
			use := `\b` + name + `\b`
			var decl *regexp.Regexp
			shadow := regexp.MustCompile(`\b(?:enum|struct|trait|type|union|mod|const|static)\s+` + name + `\b`)
			switch kind {
			case "fn":
				use, decl, shadow = `\b`+name+`\s*(?:::<[^>]*>\s*)?\(`, regexp.MustCompile(`\bfn\s+`+name+`\b`), nil
			case "enum":
				use = `\b` + name + `::\w`
			case "mod":
				use = `\b` + name + `::`
			}
			if kind == "struct" && strings.Contains(line, "{") {
				owner = name
			}
			pattern := regexp.MustCompile(use)
			items = append(items, callerItem{label: kind + " " + name, word: name, file: rel, near: pattern, far: pattern, decl: decl, shadow: shadow})
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
		more := ""
		if site.extra > 0 {
			more = fmt.Sprintf(" (+%d more)", site.extra)
		}
		lines = append(lines, fmt.Sprintf("    %s:%d: %s%s", site.file, site.line, site.text, more))
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
