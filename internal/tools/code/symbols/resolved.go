package symbols

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/tools/code/lsp"
	"tofu/internal/tools/code/search/engine"
)

type declared struct {
	at     lsp.Location
	kind   string
	symbol string
	used   lsp.Usages
}

func (d declared) label() string { return d.kind + " " + d.symbol }

type answer struct {
	server      lsp.Server
	definitions []declared
	statuses    []string
}

func (s Symbols) Serving(servers *lsp.Pool) Symbols {
	s.servers = servers
	return s
}

func (s Symbols) resolve(ctx context.Context, name string, files []string) ([]string, []string) {
	resolved := map[string]bool{}
	var blocks []string
	for _, server := range lsp.Servers() {
		mine := slices.DeleteFunc(slices.Clone(files), func(rel string) bool {
			served, serves := lsp.ServerFor(rel)
			return !serves || served.Program != server.Program || !slices.Contains([]string{".rs", ".ts", ".tsx", ".py"}, filepath.Ext(rel))
		})
		definitions := s.declarations(name, mine)
		if len(definitions) == 0 {
			continue
		}
		found, err := s.ask(ctx, server, definitions, mine)
		if err != nil {
			lost := "these " + server.Language + " call sites are matched on the name alone and a call through an import alias is missed"
			if server.Language == "python" {
				lost = "no python caller of " + name + " is listed"
			}
			blocks = append(blocks, fmt.Sprintf("%s: %v. until it answers, %s\n", server.Language, err, lost))
			continue
		}
		resolved[server.Program] = true
		blocks = append(blocks, s.render(name, found))
	}
	left := slices.DeleteFunc(slices.Clone(files), func(rel string) bool {
		served, serves := lsp.ServerFor(rel)
		return serves && resolved[served.Program]
	})
	return left, blocks
}

func (s Symbols) declarations(name string, files []string) []declared {
	var found []declared
	var scanned []string
	pythonHead := regexp.MustCompile(`^([ \t]*)(?:async[ \t]+)?(def|class)[ \t]+([A-Za-z_]\w*)`)
	for _, rel := range files {
		if filepath.Ext(rel) != ".py" {
			scanned = append(scanned, rel)
			continue
		}
		type class struct {
			indent int
			name   string
		}
		var classes []class
		for number, line := range lsp.Lines(s.abs(rel)) {
			head := pythonHead.FindStringSubmatch(line)
			if head == nil {
				continue
			}
			classes = slices.DeleteFunc(classes, func(outer class) bool { return outer.indent >= len(head[1]) })
			symbol := head[3]
			if len(classes) > 0 {
				symbol = classes[len(classes)-1].name + "." + head[3]
			}
			if head[3] == name {
				found = append(found, declared{at: s.at(rel, number+1, line[:len(head[0])-len(name)]), kind: head[2], symbol: symbol})
			}
			if head[2] == "class" {
				classes = append(classes, class{indent: len(head[1]), name: head[3]})
			}
		}
	}
	graph, err := engine.Symbols(string(s.root), scanned, name)
	if err != nil {
		return found
	}
	for _, definition := range graph.Definitions {
		text := lsp.Lines(s.abs(definition.Path))
		for number := definition.FirstLine; number <= min(definition.LastLine, len(text)); number++ {
			if column := lsp.WordAt(text[number-1], name); column >= 0 {
				found = append(found, declared{at: s.at(definition.Path, number, text[number-1][:column]), kind: string(definition.Kind), symbol: definition.Symbol})
				break
			}
		}
	}
	return found
}

func (s Symbols) abs(rel string) string {
	return filepath.Join(string(s.root), filepath.FromSlash(rel))
}

func (s Symbols) at(rel string, line int, before string) lsp.Location {
	return lsp.Location{Path: s.abs(rel), Line: line, Character: lsp.Units(before)}
}

func (s Symbols) ask(ctx context.Context, server lsp.Server, definitions []declared, files []string) (answer, error) {
	found := answer{server: server, definitions: definitions}
	byRoot := map[string][]*declared{}
	for at := range found.definitions {
		root := server.RootOf(string(s.root), found.definitions[at].at.Path)
		byRoot[root] = append(byRoot[root], &found.definitions[at])
	}
	for root, here := range byRoot {
		err := s.servers.Use(server, root, func(client *lsp.Client) error {
			if err := client.Open(s.under(server, root, files)...); err != nil {
				return err
			}
			for _, definition := range here {
				asking, cancel := context.WithTimeout(ctx, konst.LspRequestMillis*time.Millisecond)
				used, err := client.Usages(asking, definition.at)
				cancel()
				if err != nil {
					return err
				}
				definition.used = used
			}
			if status := client.Status(); status != "" {
				found.statuses = append(found.statuses, status)
			}
			return nil
		})
		if err != nil {
			return answer{}, err
		}
	}
	return found, nil
}

func (s Symbols) under(server lsp.Server, root string, files []string) []string {
	var opened []string
	for _, rel := range files {
		_, served := server.Extensions[filepath.Ext(rel)]
		if within, err := filepath.Rel(root, s.abs(rel)); served && err == nil && !strings.HasPrefix(within, "..") && len(opened) < konst.LspOpenedFiles {
			opened = append(opened, s.abs(rel))
		}
	}
	return opened
}

func (s Symbols) render(name string, found answer) string {
	var calls, others int
	for _, definition := range found.definitions {
		calls, others = calls+len(definition.used.Calls), others+len(definition.used.Others)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s in %s, resolved by %s: %s, %s, %s\n", name, found.server.Language, found.server.Program,
		counted(len(found.definitions), "definition"), counted(calls, "caller"), counted(others, "other reference"))
	for _, status := range found.statuses {
		fmt.Fprintf(&out, "%s says: %s\n", found.server.Program, status)
	}
	for _, definition := range found.definitions {
		fmt.Fprintf(&out, "\ndefined %s:%d %s\n", s.shown(definition.at.Path), definition.at.Line, definition.label())
		if len(definition.used.Calls) == 0 {
			fmt.Fprintf(&out, "  nothing calls it: 0 callers\n")
		} else {
			fmt.Fprintf(&out, "  called from %s\n", counted(len(definition.used.Calls), "place"))
		}
		for _, call := range definition.used.Calls {
			fmt.Fprintf(&out, "    %s:%d in %s\n", s.shown(call.Path), call.Line, cmp.Or(call.Within, "the file scope"))
		}
		listed := definition.used.Others
		if len(listed) > 0 {
			fmt.Fprintf(&out, "  referenced %s, not as a call\n", counted(len(listed), "more time"))
		}
		for _, reference := range listed[:min(len(listed), konst.SymbolsReferencesShown)] {
			fmt.Fprintf(&out, "    %s:%d %s\n", s.shown(reference.Path), reference.Line, preview(reference))
		}
		if hidden := len(listed) - konst.SymbolsReferencesShown; hidden > 0 {
			fmt.Fprintf(&out, "    and %d more, not shown\n", hidden)
		}
	}
	return out.String()
}

func (s Symbols) shown(path string) string {
	rel, err := filepath.Rel(string(s.root), path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(rel)
}

func preview(at lsp.Location) string {
	read := lsp.Lines(at.Path)
	if at.Line > len(read) {
		return ""
	}
	text := []rune(strings.TrimSpace(read[at.Line-1]))
	if len(text) > konst.SymbolsPreviewChars {
		return string(text[:konst.SymbolsPreviewChars]) + "..."
	}
	return string(text)
}

func counted(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
